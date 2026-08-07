package stats

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeOpenCodeDBFixture(t *testing.T, home string, rows []string) string {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("LOCALAPPDATA", "")
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not found")
	}
	dir := filepath.Join(home, ".local", "share", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "opencode.db")
	sql := `DROP TABLE IF EXISTS message;
	CREATE TABLE message (
		id text PRIMARY KEY,
		session_id text NOT NULL,
		time_created integer NOT NULL,
		time_updated integer NOT NULL,
		data text NOT NULL
	);`
	for i, row := range rows {
		sql += "INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES ('msg-" + string(rune('a'+i)) + "', 'ses-1', 0, 0, '" + row + "');\n"
	}
	if out, err := exec.Command(sqlite3, dbPath, sql).CombinedOutput(); err != nil {
		t.Fatalf("create opencode fixture: %v\n%s", err, out)
	}
	return dbPath
}

func openCodeMsg(role, model string, cost *float64, tin, tout, trea, tcr, tcw int64, createdMs int64, cwd string) string {
	fields := []string{
		`"role":` + jsonQuote(role),
		`"modelID":` + jsonQuote(model),
	}
	if cost != nil {
		fields = append(fields, `"cost":`+jsonNumber(*cost))
	}
	fields = append(fields,
		`"tokens":{"input":`+jsonNumber(float64(tin))+`,"output":`+jsonNumber(float64(tout))+
			`,"reasoning":`+jsonNumber(float64(trea))+`,"cache":{"read":`+jsonNumber(float64(tcr))+
			`,"write":`+jsonNumber(float64(tcw))+`}}`,
		`"time":{"created":`+jsonNumber(float64(createdMs))+`}`,
	)
	if cwd != "" {
		fields = append(fields, `"path":{"cwd":`+jsonQuote(cwd)+`}`)
	}
	return "{" + strings.Join(fields, ",") + "}"
}

func jsonNumber(v float64) string {
	encoded, _ := json.Marshal(v)
	return string(encoded)
}

func jsonQuote(v string) string {
	encoded, _ := json.Marshal(v)
	return string(encoded)
}

func TestCollectOpenCodeStatsAggregatesObservedFacts(t *testing.T) {
	setTestLocalLocation(t, time.UTC)
	home := t.TempDir()
	// 2026-08-07 06:00 UTC and 2026-08-06 06:00 UTC in epoch ms.
	day2 := int64(1786082400000) // 2026-08-07 06:00:00 UTC
	day1 := int64(1785996000000) // 2026-08-06 06:00:00 UTC

	cost0015 := 0.0015
	zeroCost := 0.0
	writeOpenCodeDBFixture(t, home, []string{
		// deepseek-v4-flash: two requests on day2, one on day1.
		openCodeMsg("assistant", "deepseek-v4-flash", &cost0015, 1000, 200, 50, 500, 10, day2, "/home/user/proj-zen"),
		openCodeMsg("assistant", "deepseek-v4-flash", &cost0015, 2000, 300, 0, 800, 0, day2, "/home/user/proj-zen"),
		openCodeMsg("assistant", "deepseek-v4-flash", &cost0015, 400, 100, 20, 300, 5, day1, "/home/user/proj-other"),
		// kimi-k2.5-free: one request on day2, cost recorded as 0.
		openCodeMsg("assistant", "kimi-k2.5-free", &zeroCost, 300, 40, 10, 0, 0, day2, "/home/user/proj-zen"),
		// user message: never a usage row.
		openCodeMsg("user", "", nil, 0, 0, 0, 0, 0, day2, "/home/user/proj-zen"),
		// assistant message without any token usage: not a usage row.
		openCodeMsg("assistant", "hy3-preview-free", &zeroCost, 0, 0, 0, 0, 0, day2, "/home/user/proj-zen"),
		// assistant message without a cost field: cost stays unknown.
		openCodeMsg("assistant", "glm-4.7-free", nil, 600, 80, 30, 0, 0, day2, "/home/user/proj-zen"),
	})

	c := &Collector{now: time.Now}
	byDate := c.collectOpenCodeStats(home)

	if len(byDate) != 2 {
		t.Fatalf("dates = %v, want 2", byDate)
	}

	day2Agg := byDate["2026-08-07"]
	if day2Agg == nil {
		t.Fatal("missing 2026-08-07 aggregation")
	}
	deepseek := day2Agg.models["deepseek-v4-flash"]
	if deepseek.sessions != 2 {
		t.Fatalf("deepseek requests = %d, want 2", deepseek.sessions)
	}
	if deepseek.inputTokens != 3000 || deepseek.outputTokens != 500 || deepseek.reasoning != 50 ||
		deepseek.cacheRead != 1300 || deepseek.cacheCreate != 10 {
		t.Fatalf("deepseek tokens = %#v", deepseek)
	}
	if deepseek.cost != 0.003 || !deepseek.costRecorded || deepseek.costUnknown {
		t.Fatalf("deepseek cost = %#v", deepseek)
	}
	if kimi := day2Agg.models["kimi-k2.5-free"]; kimi.sessions != 1 || kimi.inputTokens != 300 ||
		kimi.cost != 0 || !kimi.costRecorded || kimi.costUnknown {
		t.Fatalf("kimi = %#v", kimi)
	}
	if _, ok := day2Agg.models["hy3-preview-free"]; ok {
		t.Fatal("zero-usage assistant row must not create a model")
	}
	glm := day2Agg.models["glm-4.7-free"]
	if glm.sessions != 1 || glm.inputTokens != 600 || !glm.costRecorded || !glm.costUnknown || glm.cost != 0 {
		t.Fatalf("missing-cost model must stay cost unknown: %#v", glm)
	}

	day1Agg := byDate["2026-08-06"]
	deepseekDay1 := day1Agg.models["deepseek-v4-flash"]
	if deepseekDay1.sessions != 1 || deepseekDay1.inputTokens != 400 {
		t.Fatalf("deepseek day1 = %#v", deepseekDay1)
	}

	// Project attribution.
	zenProject := day2Agg.projects["proj-zen"]
	if zenProject == nil || zenProject.sessions != 4 {
		t.Fatalf("proj-zen project = %#v", zenProject)
	}
	if zenProject.cost != 0.003 || zenProject.inputTokens != 3000+300+600 || !zenProject.costUnknown {
		t.Fatalf("proj-zen cost/input = %#v", zenProject)
	}
	otherProject := day2Agg.projects["proj-other"]
	if otherProject != nil {
		t.Fatalf("proj-other must only exist on day1: %#v", otherProject)
	}

	// Slot attribution: 06:00 UTC is hour 6 -> slot 1 on both days.
	if day2Agg.slots[1].sessions != 4 || day2Agg.slots[1].totalTokens != 3000+500+50+1300+10+300+40+10+600+80+30 {
		t.Fatalf("slot 1 = %#v", day2Agg.slots[1])
	}
	if day1Agg.slots[1].sessions != 1 {
		t.Fatalf("slot 1 day1 = %#v", day1Agg.slots[1])
	}
}

func TestCollectOpenCodeStatsStableAndReload(t *testing.T) {
	setTestLocalLocation(t, time.UTC)
	home := t.TempDir()
	created := int64(1786082400000)
	cost0015 := 0.0015

	writeOpenCodeDBFixture(t, home, []string{
		openCodeMsg("assistant", "deepseek-v4-flash", &cost0015, 1000, 200, 50, 500, 10, created, "/home/user/proj-zen"),
		openCodeMsg("assistant", "kimi-k2.5-free", &cost0015, 300, 40, 0, 0, 0, created, "/home/user/proj-zen"),
	})

	c := &Collector{now: time.Now}
	first := c.collectOpenCodeStats(home)
	second := c.collectOpenCodeStats(home)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("repeated collection must be stable")
	}

	// Simulate new OpenCode activity: the CLI has written more rows — a new
	// deepseek request on day2, a new day, and a new model. Re-reading the
	// database must pick them up without stale state.
	writeOpenCodeDBFixture(t, home, []string{
		openCodeMsg("assistant", "deepseek-v4-flash", &cost0015, 1000, 200, 50, 500, 10, created, "/home/user/proj-zen"),
		openCodeMsg("assistant", "deepseek-v4-flash", &cost0015, 2500, 400, 100, 900, 0, created, "/home/user/proj-zen"),
		openCodeMsg("assistant", "deepseek-v4-flash", &cost0015, 400, 100, 20, 300, 5, int64(1785996000000), "/home/user/proj-new"),
		openCodeMsg("assistant", "kimi-k2.5-free", &cost0015, 300, 40, 0, 0, 0, created, "/home/user/proj-zen"),
		openCodeMsg("assistant", "qwen3.6-plus-free", &cost0015, 700, 90, 0, 0, 0, created, "/home/user/proj-zen"),
	})

	third := c.collectOpenCodeStats(home)
	day2 := third["2026-08-07"]
	if day2 == nil || day2.models["deepseek-v4-flash"].sessions != 2 {
		t.Fatalf("reloaded deepseek = %#v", day2)
	}
	if day2.models["deepseek-v4-flash"].inputTokens != 3500 {
		t.Fatalf("reloaded deepseek input = %#v", day2.models["deepseek-v4-flash"])
	}
	if day2.models["qwen3.6-plus-free"].sessions != 1 {
		t.Fatalf("reloaded qwen = %#v", day2.models["qwen3.6-plus-free"])
	}
	day1 := third["2026-08-06"]
	if day1 == nil || day1.models["deepseek-v4-flash"].sessions != 1 || day1.models["deepseek-v4-flash"].inputTokens != 400 {
		t.Fatalf("reloaded day1 = %#v", day1)
	}
	if day1.projects["proj-new"] == nil || day1.projects["proj-new"].sessions != 1 {
		t.Fatalf("reloaded project = %#v", day1.projects)
	}
}

func TestCollectOpenCodeStatsMissingFieldsAndBadRows(t *testing.T) {
	setTestLocalLocation(t, time.UTC)
	home := t.TempDir()
	created := int64(1786082400000)

	// Missing cache.write, missing cost, missing cwd, malformed JSON, and a
	// row with an unparseable timestamp must all degrade to observed facts.
	rows := []string{
		`{"role":"assistant","modelID":"model-a","tokens":{"input":100,"output":20},"time":{"created":` + jsonNumber(float64(created)) + `}}`,
		`{"role":"assistant","modelID":"model-a","tokens":{"input":50,"output":10,"cache":{"read":30}},"cost":0.0001,"time":{"created":` + jsonNumber(float64(created)) + `}}`,
		`{`,
		`{"role":"assistant","modelID":"model-b","tokens":{"input":1,"output":1},"cost":0.0002,"time":{"created":"not-a-time"}}`,
	}
	writeOpenCodeDBFixture(t, home, rows)

	c := &Collector{now: time.Now}
	byDate := c.collectOpenCodeStats(home)

	agg := byDate["2026-08-07"]
	if agg == nil {
		t.Fatal("missing aggregation")
	}
	modelA := agg.models["model-a"]
	if modelA.sessions != 2 || modelA.inputTokens != 150 || modelA.outputTokens != 30 || modelA.cacheRead != 30 || modelA.cacheCreate != 0 {
		t.Fatalf("model-a = %#v", modelA)
	}
	if !modelA.costRecorded || modelA.cost != 0.0001 || !modelA.costUnknown {
		t.Fatalf("model-a cost must come only from the row that has it and stay partly unknown: %#v", modelA)
	}
	if _, ok := agg.models["model-b"]; ok {
		t.Fatal("unparseable timestamp must not create a usage row")
	}
	if len(agg.projects) != 0 {
		t.Fatalf("no cwd must not create projects: %#v", agg.projects)
	}
}

func TestCollectOpenCodeStatsToleratesMalformedRows(t *testing.T) {
	setTestLocalLocation(t, time.UTC)
	home := t.TempDir()
	created := int64(1786082400000)
	writeOpenCodeDBFixture(t, home, []string{
		`{`,
		`not json at all`,
		openCodeMsg("assistant", "deepseek-v4-flash", nil, 100, 20, 0, 0, 0, created, ""),
	})

	c := &Collector{now: time.Now}
	byDate := c.collectOpenCodeStats(home)
	agg := byDate["2026-08-07"]
	if agg == nil || agg.models["deepseek-v4-flash"].sessions != 1 {
		t.Fatalf("malformed rows must be skipped without failing the read: %#v", byDate)
	}
}

func TestCollectOpenCodeStatsAbsentDatabase(t *testing.T) {
	home := t.TempDir()
	c := &Collector{now: time.Now}
	if got := c.collectOpenCodeStats(home); len(got) != 0 {
		t.Fatalf("absent database must yield no rows: %#v", got)
	}
}

func TestOpenCodeModelsFlowIntoStatsPayload(t *testing.T) {
	setTestLocalLocation(t, time.UTC)
	home := t.TempDir()
	created := int64(1786082400000)
	cost0015 := 0.0015
	writeOpenCodeDBFixture(t, home, []string{
		openCodeMsg("assistant", "deepseek-v4-flash", &cost0015, 1000, 200, 50, 500, 10, created, "/home/user/proj-zen"),
		openCodeMsg("assistant", "kimi-k2.5-free", &cost0015, 300, 40, 0, 0, 0, created, "/home/user/proj-zen"),
	})

	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	c := NewCollector()
	c.refresh()

	all := c.Stats().Ranges["all"]
	var names []string
	for _, m := range all.Models {
		names = append(names, m.Name)
	}
	got := map[string]bool{}
	for _, name := range names {
		got[name] = true
	}
	if !got["deepseek-v4-flash"] {
		t.Fatalf("deepseek-v4-flash missing from payload models: %v", names)
	}
	if !got["kimi-k2.5-free"] {
		t.Fatalf("kimi-k2.5-free missing from payload models: %v", names)
	}
	for _, m := range all.Models {
		if m.Name == "deepseek-v4-flash" {
			if m.Sessions != 1 || m.InputTokens != 1000 || m.Cost != 0.0015 || !m.CostKnown {
				t.Fatalf("deepseek payload row = %#v", m)
			}
		}
	}
}
