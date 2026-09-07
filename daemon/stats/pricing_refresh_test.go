package stats

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func isolatePricing(t *testing.T) {
	t.Helper()
	prices.mu.Lock()
	models, at, source := prices.models, prices.updatedAt, prices.source
	prices.models = clonePricingMap(staticPricing)
	prices.updatedAt = time.Now()
	prices.source = "built-in"
	prices.mu.Unlock()
	url, client := pricingSyncURL, pricingHTTPClient
	t.Cleanup(func() {
		prices.mu.Lock()
		prices.models, prices.updatedAt, prices.source = models, at, source
		prices.mu.Unlock()
		pricingSyncURL, pricingHTTPClient = url, client
	})
}

func pricingServer(t *testing.T, payload *string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, *payload)
	}))
	t.Cleanup(srv.Close)
	pricingSyncURL, pricingHTTPClient = srv.URL, srv.Client()
}

const newModelCatalog = `{"openai":{"models":{"new-exact-model":{"name":"New model","cost":{"input":2,"output":4,"cache_read":0,"cache_write":0}}}}}`

func TestPricingFreshCacheConcurrentMissesAndCooldown(t *testing.T) {
	isolatePricing(t)
	r := newPricingRefresher()
	now := time.Now()
	r.now = func() time.Time { return now }
	calls := 0
	r.sync = func(context.Context, string) error { calls++; return nil }
	r.step(context.Background(), "", func() {})
	if calls != 0 {
		t.Fatal("fresh cache fetched without a miss")
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.observe([]string{"new-exact-model"}) }()
	}
	wg.Wait()
	if len(r.wake) != 1 {
		t.Fatal("misses must coalesce")
	}
	r.step(context.Background(), "", func() {})
	if calls != 1 {
		t.Fatalf("new model with fresh cache: %d calls", calls)
	}
	for i := 0; i < 100; i++ {
		r.observe([]string{"new-exact-model"})
		r.step(context.Background(), "", func() {})
	}
	if calls != 1 {
		t.Fatal("repeated unknown caused a storm")
	}
	r.observe([]string{"another-exact-model"})
	r.step(context.Background(), "", func() {})
	if calls != 1 {
		t.Fatal("global cooldown bypassed")
	}
	now = now.Add(5 * time.Minute)
	r.step(context.Background(), "", func() {})
	if calls != 2 {
		t.Fatal("new pending model was lost")
	}
	now = now.Add(time.Hour)
	r.observe([]string{"new-exact-model"})
	r.step(context.Background(), "", func() {})
	if calls != 3 {
		t.Fatal("negative cache did not expire")
	}
}

func TestPricingFailureBackoffRecoveryAndCancellation(t *testing.T) {
	isolatePricing(t)
	r := newPricingRefresher()
	now := time.Now()
	r.now = func() time.Time { return now }
	fail, calls := true, 0
	r.sync = func(context.Context, string) error {
		calls++
		if fail {
			return errors.New("offline")
		}
		return nil
	}
	r.observe([]string{"unknown"})
	for _, delay := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour} {
		r.step(context.Background(), "", func() {})
		if r.next != now.Add(delay) || r.status().LastError == "" {
			t.Fatal("missing backoff/error")
		}
		r.step(context.Background(), "", func() { t.Fatal("failed fetch recomputed") })
		now = now.Add(delay)
	}
	if calls != 4 {
		t.Fatalf("unexpected retries: %d", calls)
	}
	fail = false
	r.step(context.Background(), "", func() {})
	if calls != 5 || r.status().LastError != "" {
		t.Fatal("recovery not published")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.step(ctx, "", func() { t.Fatal("cancelled refresh") })
	if calls != 5 {
		t.Fatal("cancelled request sent")
	}
}

func TestPricingInvalidCatalogRetainsLastGood(t *testing.T) {
	isolatePricing(t)
	payload := newModelCatalog
	pricingServer(t, &payload)
	home := t.TempDir()
	if err := syncPricing(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	before := clonePricingMap(prices.models)
	at := prices.updatedAt
	disk, err := os.ReadFile(pricingCachePath(home))
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		"{", "{}", "null", `{"other":{"models":{"a":{"cost":{"input":1}}}}}`,
		`{"openai":{"models":{"a":{"cost":{"input":-1}}}}}`,
		`{"openai":{"models":{"a":{"cost":{"input":1}}}},"xai":{"models":{"a":{"cost":{"input":2}}}}}`,
		newModelCatalog + "{}",
		`{"openai":{"models":{"claude-sonnet-4-6":{"cost":{"input":99}}}}}`,
	} {
		payload = invalid
		if err := syncPricing(context.Background(), home); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
		if !reflect.DeepEqual(before, prices.models) || at != prices.updatedAt {
			t.Fatal("last good registry replaced")
		}
		got, _ := os.ReadFile(pricingCachePath(home))
		if string(got) != string(disk) {
			t.Fatal("last good disk replaced")
		}
	}
}

func TestPricingMissingRatesZeroAndTiers(t *testing.T) {
	isolatePricing(t)
	payload := `{"openai":{"models":{
		"partial":{"cost":{"input":2}},
		"free":{"cost":{"input":0,"output":0}},
		"tiered":{"cost":{"input":2,"tiers":[{"input":4,"tier":{"type":"context","size":100}}]}}
	}}}`
	pricingServer(t, &payload)
	home := t.TempDir()
	if err := syncPricing(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id            string
		input, output int64
		known         bool
		cost          float64
	}{
		{"partial", 1000000, 0, true, 2},
		{"partial", 1000000, 1, false, 0},
		{"free", 100, 100, true, 0},
		{"tiered", 100, 0, false, 0},
		{"openai/partial", 100, 0, false, 0},
	} {
		got, known := computeKnownCost(tc.id, tc.input, tc.output, 0, 0, 0)
		if got != tc.cost || known != tc.known {
			t.Fatalf("%s: %v/%v", tc.id, got, known)
		}
	}
	loadPricingCache(home)
	if _, known := computeKnownCost("partial", 1, 1, 0, 0, 0); known {
		t.Fatal("cache lost rate presence")
	}
	if _, known := computeKnownCost("tiered", 1, 0, 0, 0, 0); known {
		t.Fatal("cache lost tiers")
	}
}

type pricingRoundTrip func(*http.Request) (*http.Response, error)

func (f pricingRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPricingTimeoutCancellationAndSizeLimit(t *testing.T) {
	isolatePricing(t)
	pricingHTTPClient = &http.Client{Transport: pricingRoundTrip(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 15*time.Second {
			t.Error("missing bounded timeout")
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := syncPricing(ctx, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err := syncPricing(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	pricingHTTPClient = &http.Client{Transport: pricingRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(io.LimitReader(zeroPricingReader{}, pricingMaxBytes+1))}, nil
	})}
	if err := syncPricing(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("size: %v", err)
	}
}

type zeroPricingReader struct{}

func (zeroPricingReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestPricingLoopCancelsInflightAndObservationsNeverWaitForHTTP(t *testing.T) {
	isolatePricing(t)
	r := newPricingRefresher()
	started := make(chan struct{})
	r.sync = func(ctx context.Context, _ string) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); r.run(ctx, "", func() { t.Error("cancelled fetch recomputed") }) }()
	r.observe([]string{"unknown"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fetch never started")
	}
	observed := make(chan struct{})
	go func() {
		defer close(observed)
		for i := 0; i < 100; i++ {
			r.observe([]string{"another-unknown"})
		}
	}()
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("observations waited for HTTP")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("owner loop leaked after cancellation")
	}
}

func TestPricingDailyFreshness(t *testing.T) {
	isolatePricing(t)
	r := newPricingRefresher()
	calls := 0
	r.sync = func(context.Context, string) error { calls++; return nil }
	prices.mu.Lock()
	prices.updatedAt = time.Now().Add(-25 * time.Hour)
	prices.mu.Unlock()
	r.step(context.Background(), "", func() {})
	if calls != 1 {
		t.Fatal("daily freshness failed without usage/discovery")
	}
}

func TestPricingKnownUsageDoesNotTriggerDiscoveryRefresh(t *testing.T) {
	isolatePricing(t)
	r := newPricingRefresher()
	r.observe([]string{"gpt-5.5"})
	if r.pending {
		t.Fatal("known basic model rates should not trigger discovery refresh")
	}
	if _, known := computeKnownCost("gpt-5.5", 1, 1, 0, 0, 1); known {
		t.Fatal("missing cache-create rate treated as free")
	}
	r.request([]string{"gpt-5.5"})
	if !r.pending {
		t.Fatal("actual unpriced cache usage must trigger refresh")
	}
}

func TestPricingReportedChargesRemainSeparateFromReferenceEstimate(t *testing.T) {
	isolatePricing(t)
	payload := newModelCatalog
	pricingServer(t, &payload)
	if err := syncPricing(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	entries := map[string]modelAggEntry{"new-exact-model": recordedEntry("new-exact-model", 0.75, 100)}
	mergeModelAgg(entries, map[string]modelAggEntry{"new-exact-model": unrecordedEntry("new-exact-model", 1000000)})
	got := buildModelStats(entries)[0]
	if got.ReportedCost != 0.75 || got.EstimatedCost != 2 || got.Cost != 2.75 || !got.CostKnown {
		t.Fatalf("mixed cost provenance: %+v", got)
	}
}

func TestPricingUnpricedReasonDistinguishesCatalogFromContext(t *testing.T) {
	isolatePricing(t)
	prices.mu.Lock()
	prices.models["context-required"] = modelPricing{source: "models.dev", present: rateInput | rateOutput, input: 2, output: 4, tiered: true}
	prices.models["rate-required"] = modelPricing{source: "models.dev", present: rateInput, input: 2}
	prices.mu.Unlock()
	for _, tc := range []struct{ id, reason string }{
		{"absent", "missing_model"}, {"context-required", "insufficient_context"}, {"rate-required", "missing_rate"},
	} {
		rows := buildModelStats(map[string]modelAggEntry{tc.id: {inputTokens: 10, outputTokens: 10}})
		if rows[0].CostKnown || rows[0].UnpricedReason != tc.reason {
			t.Fatalf("%s: %+v", tc.id, rows[0])
		}
	}
	rows := buildModelStats(map[string]modelAggEntry{"context-required": recordedEntry("context-required", 0.5, 10)})
	if !rows[0].CostKnown || rows[0].UnpricedReason != "" || rows[0].ReportedCost != 0.5 {
		t.Fatalf("recorded charges lost: %+v", rows[0])
	}
}

func TestPricingWriteFailureRetainsRegistryAndCleansTemporaryFile(t *testing.T) {
	isolatePricing(t)
	payload := newModelCatalog
	pricingServer(t, &payload)
	home := t.TempDir()
	// A directory at the destination forces rename failure after the temp file is synced.
	if err := os.MkdirAll(pricingCachePath(home), 0700); err != nil {
		t.Fatal(err)
	}
	before := clonePricingMap(prices.models)
	if err := syncPricing(context.Background(), home); err == nil {
		t.Fatal("write failure hidden")
	}
	if !reflect.DeepEqual(before, prices.models) {
		t.Fatal("unpersisted registry published")
	}
	files, _ := filepath.Glob(filepath.Join(home, ".zen", ".pricing-*.tmp"))
	if len(files) != 0 {
		t.Fatal("temporary files leaked")
	}
}

func TestPricingRecomputesCachedUsageAndExposesProvenance(t *testing.T) {
	isolatePricing(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "test")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	usage := `{"type":"assistant","timestamp":"2026-09-06T00:00:00Z","message":{"id":"m1","model":"new-exact-model","usage":{"input_tokens":1000000,"output_tokens":0}}}`
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(usage+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := NewCollector()
	c.refresh()
	if c.Stats().Ranges["all"].CostKnown {
		t.Fatal("unknown usage treated as free")
	}
	payload := newModelCatalog
	pricingServer(t, &payload)
	c.pricingRefresh.step(context.Background(), home, c.refresh)
	got := c.Stats()
	if !got.Ranges["all"].CostKnown || got.Ranges["all"].Cost != 2 {
		t.Fatalf("cached total not recalculated: %+v", got.Ranges["all"])
	}
	m := got.Ranges["all"].Models[0]
	if m.ID != "new-exact-model" || m.EstimatedCost != 2 || m.ReportedCost != 0 || m.EstimateSource != "models.dev" || m.ReferenceProvider != "openai" {
		t.Fatalf("provenance: %+v", m)
	}
	if !strings.Contains(got.Pricing.Basis, "not actual gateway bills") {
		t.Fatal("billing claim")
	}
	count := 0
	c.pricingRefresh.next = time.Time{}
	c.pricingRefresh.pending = true
	c.pricingRefresh.step(context.Background(), home, func() { count++ })
	if count != 0 {
		t.Fatal("unchanged rates rescanned usage")
	}
}
