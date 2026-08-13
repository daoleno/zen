package modelprofiles

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// Defect regression: Codex >= 0.147 sends POST /v1/responses bodies with
// Content-Encoding: gzip. The router must decompress at the boundary, rewrite
// the model on plain JSON, and forward without the original encoding header.
func TestRouterGzipResponsesBodyForwarded(t *testing.T) {
	for _, encoding := range []string{"gzip", "x-gzip"} {
		t.Run(encoding, func(t *testing.T) {
			upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Content-Encoding"); got != "" {
					t.Errorf("upstream must not see Content-Encoding, got %q", got)
				}
				body, _ := io.ReadAll(r.Body)
				var obj map[string]any
				if err := json.Unmarshal(body, &obj); err != nil {
					t.Fatalf("upstream body is not plain JSON: %v", err)
				}
				if obj["model"] != "upstream-model-v2" {
					t.Errorf("model=%v", obj["model"])
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			})
			defer upstream.Close()

			table := NewRouteTable()
			profile := routedCodex(upstream.URL, "gpt-5", "upstream-model-v2")
			state, err := table.BindLaunch("s1", profile, 1, verifiedAuth(profile))
			if err != nil {
				t.Fatal(err)
			}
			router := NewRouter(table)
			srv := httptest.NewServer(router.Handler())
			defer srv.Close()
			base, err := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)
			if err != nil {
				t.Fatal(err)
			}

			var buf bytes.Buffer
			zw := gzip.NewWriter(&buf)
			if _, err := zw.Write([]byte(`{"model":"cli-picked","input":"hi"}`)); err != nil {
				t.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}

			req, err := http.NewRequest(http.MethodPost, base+"/responses", &buf)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Encoding", encoding)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
			}
		})
	}
}

// The decompressed size is bounded independently of the compressed size: a
// small gzip bomb past MaxRouteRequestBodyBytes must yield 413, not OOM.
func TestRouterGzipBombDecompressedBounded(t *testing.T) {
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("bomb must not reach upstream")
	})
	defer upstream.Close()
	table := NewRouteTable()
	profile := routedCodex(upstream.URL, "gpt-5", "up-m")
	state, err := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table, WithRouterMaxBody(1024))
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(`{"model":"x","input":"` + strings.Repeat("a", 4096) + `"}`)); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()

	req, _ := http.NewRequest(http.MethodPost, base+"/responses", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d want 413", resp.StatusCode)
	}
}

// A gzip Content-Encoding with non-gzip bytes is malformed, not a pass-through.
func TestRouterGzipMalformedBodyRejected(t *testing.T) {
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("malformed body must not reach upstream")
	})
	defer upstream.Close()
	table := NewRouteTable()
	profile := routedCodex(upstream.URL, "gpt-5", "up-m")
	state, err := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

	req, _ := http.NewRequest(http.MethodPost, base+"/responses", strings.NewReader(`{"model":"x"}`))
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "request_body_malformed") {
		t.Fatalf("error code missing: %s", raw)
	}
}

// Owner-level end-to-end: a gzip-encoded /v1/responses POST through the real
// loopback listener (what Codex >= 0.147 does) reaches the upstream as plain
// rewritten JSON and returns 200.
func TestOwnerRouterServesGzipResponsesBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Encoding"); got != "" {
			t.Errorf("upstream Content-Encoding=%q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var obj map[string]any
		if err := json.Unmarshal(body, &obj); err != nil {
			t.Fatalf("upstream body not plain JSON: %v", err)
		}
		if obj["model"] != "gpt-5.6-sol" {
			t.Errorf("upstream model=%v", obj["model"])
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	owner := startTestOwner(t, readyLookup("x"))
	if _, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "cf-api-fan", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: upstream.URL + "/v1",
		ModelID: "gpt-5.6-sol", Advanced: true,
	}, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, "cf-api-fan", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	state, _ := owner.Table().Get("s1")
	base, err := LoopbackCodexBaseURL(owner.ListenAddr(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(`{"model":"cli-picked","input":"hi"}`)); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()
	req, err := http.NewRequest(http.MethodPost, base+"/responses", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
}

// compressBodyForTest encodes payload with the given Content-Encoding using
// the same codecs a real client ships: gzip/x-gzip via compress/gzip, zstd and
// its zst alias via klauspost/compress/zstd, deflate via compress/zlib (the
// RFC 1950 zlib wrapper, which is what HTTP deflate means), deflate-raw via
// compress/flate (non-conforming raw streams some clients send), br via
// andybalholm/brotli.
func compressBodyForTest(t *testing.T, encoding, payload string) []byte {
	t.Helper()
	var buf bytes.Buffer
	switch encoding {
	case "gzip", "x-gzip":
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	case "zstd", "zst":
		zw, err := zstd.NewWriter(&buf)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := zw.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	case "deflate":
		zw := zlib.NewWriter(&buf)
		if _, err := zw.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	case "deflate-raw":
		zw, err := flate.NewWriter(&buf, flate.DefaultCompression)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := zw.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	case "br":
		zw := brotli.NewWriter(&buf)
		if _, err := zw.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported test encoding %q", encoding)
	}
	return buf.Bytes()
}

// compressStackedForTest applies codings in application order (the order they
// would appear in a Content-Encoding header), so the first coding is applied
// first and the last one ends up outermost.
func compressStackedForTest(t *testing.T, codings []string, payload string) []byte {
	t.Helper()
	data := []byte(payload)
	for _, coding := range codings {
		data = compressBodyForTest(t, coding, string(data))
	}
	return data
}

// Real Codex 0.147 sends zstd-compressed /v1/responses bodies; the router must
// decode zstd (and its zst alias), deflate (zlib-wrapped and raw), and brotli
// exactly like gzip at the boundary: upstream sees plain rewritten JSON with
// no Content-Encoding header.
func TestRouterCodecResponsesBodyForwarded(t *testing.T) {
	cases := []struct {
		header  string
		compact string
	}{
		{"zstd", "zstd"},
		{"zst", "zst"},
		{"deflate", "deflate"},
		{"deflate", "deflate-raw"},
		{"br", "br"},
	}
	for _, tc := range cases {
		t.Run(tc.header+"/"+tc.compact, func(t *testing.T) {
			upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Content-Encoding"); got != "" {
					t.Errorf("upstream must not see Content-Encoding, got %q", got)
				}
				body, _ := io.ReadAll(r.Body)
				var obj map[string]any
				if err := json.Unmarshal(body, &obj); err != nil {
					t.Fatalf("upstream body is not plain JSON: %v", err)
				}
				if obj["model"] != "upstream-model-v2" {
					t.Errorf("model=%v", obj["model"])
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			})
			defer upstream.Close()

			table := NewRouteTable()
			profile := routedCodex(upstream.URL, "gpt-5", "upstream-model-v2")
			state, err := table.BindLaunch("s1", profile, 1, verifiedAuth(profile))
			if err != nil {
				t.Fatal(err)
			}
			router := NewRouter(table)
			srv := httptest.NewServer(router.Handler())
			defer srv.Close()
			base, err := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)
			if err != nil {
				t.Fatal(err)
			}

			req, err := http.NewRequest(
				http.MethodPost,
				base+"/responses",
				bytes.NewReader(compressBodyForTest(t, tc.compact, `{"model":"cli-picked","input":"hi"}`)),
			)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Encoding", tc.header)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
			}
		})
	}
}

// Stacked codings ("gzip, zstd" = apply gzip first, then zstd) must decode in
// reverse order with the JSON rewrite applied only after the final stage.
func TestRouterStackedCodingsResponsesBodyForwarded(t *testing.T) {
	cases := []struct {
		header  string
		codings []string
	}{
		{"gzip, zstd", []string{"gzip", "zstd"}},
		{"br, zstd", []string{"br", "zstd"}},
	}
	for _, tc := range cases {
		t.Run(tc.header, func(t *testing.T) {
			upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Content-Encoding"); got != "" {
					t.Errorf("upstream must not see Content-Encoding, got %q", got)
				}
				body, _ := io.ReadAll(r.Body)
				var obj map[string]any
				if err := json.Unmarshal(body, &obj); err != nil {
					t.Fatalf("upstream body is not plain JSON: %v", err)
				}
				if obj["model"] != "upstream-model-v2" {
					t.Errorf("model=%v", obj["model"])
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			})
			defer upstream.Close()

			table := NewRouteTable()
			profile := routedCodex(upstream.URL, "gpt-5", "upstream-model-v2")
			state, err := table.BindLaunch("s1", profile, 1, verifiedAuth(profile))
			if err != nil {
				t.Fatal(err)
			}
			router := NewRouter(table)
			srv := httptest.NewServer(router.Handler())
			defer srv.Close()
			base, err := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)
			if err != nil {
				t.Fatal(err)
			}

			req, err := http.NewRequest(
				http.MethodPost,
				base+"/responses",
				bytes.NewReader(compressStackedForTest(t, tc.codings, `{"model":"cli-picked","input":"hi"}`)),
			)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Encoding", tc.header)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
			}
		})
	}
}

// Unsupported codings are rejected explicitly (fail closed): a compressed body
// the router cannot decode must never reach JSON rewriting or the upstream.
func TestRouterUnsupportedEncodingRejected(t *testing.T) {
	for _, encoding := range []string{"snappy", "compress", "gzip, snappy", "lz4"} {
		t.Run(encoding, func(t *testing.T) {
			upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("unsupported encoding body must not reach upstream")
			})
			defer upstream.Close()
			table := NewRouteTable()
			profile := routedCodex(upstream.URL, "gpt-5", "up-m")
			state, err := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
			if err != nil {
				t.Fatal(err)
			}
			router := NewRouter(table)
			srv := httptest.NewServer(router.Handler())
			defer srv.Close()
			base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

			req, _ := http.NewRequest(http.MethodPost, base+"/responses", strings.NewReader(`{"model":"x"}`))
			req.Header.Set("Content-Encoding", encoding)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
			}
			if !strings.Contains(string(raw), "request_body_malformed") {
				t.Fatalf("error code missing: %s", raw)
			}
		})
	}
}

// The decompressed size bound applies to every codec: small zstd/zst/deflate/br
// bombs past MaxRouteRequestBodyBytes must yield 413, not OOM. Stacked bombs
// are bounded at each stage, so the gzip stage of "gzip, zstd" still trips 413.
func TestRouterCodecBombDecompressedBounded(t *testing.T) {
	cases := []struct {
		name     string
		encoding string
		body     func(string) []byte
	}{
		{"zstd", "zstd", func(p string) []byte { return compressBodyForTest(t, "zstd", p) }},
		{"zst", "zst", func(p string) []byte { return compressBodyForTest(t, "zst", p) }},
		{"deflate", "deflate", func(p string) []byte { return compressBodyForTest(t, "deflate", p) }},
		{"deflate-raw", "deflate", func(p string) []byte { return compressBodyForTest(t, "deflate-raw", p) }},
		{"br", "br", func(p string) []byte { return compressBodyForTest(t, "br", p) }},
		{"stacked gzip,zstd", "gzip, zstd", func(p string) []byte { return compressStackedForTest(t, []string{"gzip", "zstd"}, p) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("bomb must not reach upstream")
			})
			defer upstream.Close()
			table := NewRouteTable()
			profile := routedCodex(upstream.URL, "gpt-5", "up-m")
			state, err := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
			if err != nil {
				t.Fatal(err)
			}
			router := NewRouter(table, WithRouterMaxBody(1024))
			srv := httptest.NewServer(router.Handler())
			defer srv.Close()
			base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

			payload := `{"model":"x","input":"` + strings.Repeat("a", 4096) + `"}`
			req, _ := http.NewRequest(http.MethodPost, base+"/responses", bytes.NewReader(tc.body(payload)))
			req.Header.Set("Content-Encoding", tc.encoding)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusRequestEntityTooLarge {
				t.Fatalf("status=%d want 413", resp.StatusCode)
			}
		})
	}
}

// A zstd/zst/deflate/br Content-Encoding with non-matching bytes is malformed,
// not a pass-through — same fail-closed contract as the gzip case.
func TestRouterCodecMalformedBodyRejected(t *testing.T) {
	for _, encoding := range []string{"zstd", "zst", "deflate", "br"} {
		t.Run(encoding, func(t *testing.T) {
			upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("malformed body must not reach upstream")
			})
			defer upstream.Close()
			table := NewRouteTable()
			profile := routedCodex(upstream.URL, "gpt-5", "up-m")
			state, err := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
			if err != nil {
				t.Fatal(err)
			}
			router := NewRouter(table)
			srv := httptest.NewServer(router.Handler())
			defer srv.Close()
			base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

			req, _ := http.NewRequest(http.MethodPost, base+"/responses", strings.NewReader(`{"model":"x"}`))
			req.Header.Set("Content-Encoding", encoding)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
			}
			if !strings.Contains(string(raw), "request_body_malformed") {
				t.Fatalf("error code missing: %s", raw)
			}
		})
	}
}

// readBoundedBody itself must decode every supported codec (plus raw-DEFLATE
// fallback and stacked codings) so non-router consumers share the fix, and
// must reject unsupported codings explicitly.
func TestReadBoundedBodyDecodesCodecs(t *testing.T) {
	cases := []struct {
		name     string
		header   string
		body     func(string) []byte
		wantBody string
	}{
		{"zstd", "zstd", func(p string) []byte { return compressBodyForTest(t, "zstd", p) }, `{"model":"m"}`},
		{"zst alias", "zst", func(p string) []byte { return compressBodyForTest(t, "zst", p) }, `{"model":"m"}`},
		{"deflate zlib", "deflate", func(p string) []byte { return compressBodyForTest(t, "deflate", p) }, `{"model":"m"}`},
		{"deflate raw fallback", "deflate", func(p string) []byte { return compressBodyForTest(t, "deflate-raw", p) }, `{"model":"m"}`},
		{"br", "br", func(p string) []byte { return compressBodyForTest(t, "br", p) }, `{"model":"m"}`},
		{"stacked gzip,zstd", "gzip, zstd", func(p string) []byte { return compressStackedForTest(t, []string{"gzip", "zstd"}, p) }, `{"model":"m"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodPost,
				"/r/x/v1/responses",
				bytes.NewReader(tc.body(tc.wantBody)),
			)
			req.Header.Set("Content-Encoding", tc.header)
			body, decoded, err := readBoundedBody(req, MaxRouteRequestBodyBytes)
			if err != nil {
				t.Fatal(err)
			}
			if !decoded {
				t.Fatalf("%s body must be decoded", tc.header)
			}
			if string(body) != tc.wantBody {
				t.Fatalf("body=%q", body)
			}
		})
	}
}

// identity (and empty) Content-Encoding must stay undecoded pass-through.
func TestReadBoundedBodyIdentityNotDecoded(t *testing.T) {
	for _, encoding := range []string{"", "identity", "Identity"} {
		t.Run(encoding, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/r/x/v1/responses", strings.NewReader(`{"model":"m"}`))
			if encoding != "" {
				req.Header.Set("Content-Encoding", encoding)
			}
			body, decoded, err := readBoundedBody(req, MaxRouteRequestBodyBytes)
			if err != nil {
				t.Fatal(err)
			}
			if decoded {
				t.Fatalf("%q must not be decoded", encoding)
			}
			if string(body) != `{"model":"m"}` {
				t.Fatalf("body=%q", body)
			}
		})
	}
}

// Unsupported codings fail closed at the body reader, not pass-through.
func TestReadBoundedBodyRejectsUnsupportedEncoding(t *testing.T) {
	for _, encoding := range []string{"snappy", "compress", "gzip, snappy"} {
		t.Run(encoding, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/r/x/v1/responses", strings.NewReader(`{"model":"m"}`))
			req.Header.Set("Content-Encoding", encoding)
			_, _, err := readBoundedBody(req, MaxRouteRequestBodyBytes)
			if err == nil {
				t.Fatalf("%q must be rejected", encoding)
			}
			if !strings.Contains(err.Error(), "unsupported content-encoding") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

// readBoundedBody itself must decode gzip so non-router consumers share the fix.
func TestReadBoundedBodyDecodesGzip(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(`{"model":"m"}`))
	_ = zw.Close()

	req := httptest.NewRequest(http.MethodPost, "/r/x/v1/responses", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	body, decoded, err := readBoundedBody(req, MaxRouteRequestBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded {
		t.Fatal("gzip body must be decoded")
	}
	if string(body) != `{"model":"m"}` {
		t.Fatalf("body=%q", body)
	}
}
