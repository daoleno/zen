package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	allTimeFixtureSkills = `[
		{"source":"vercel-labs/skills","skillId":"find-skills","name":"find-skills","installs":2580013,"weeklyInstalls":[111669,112706,118887,110834,113781,109199,109085,115475],"isOfficial":true},
		{"source":"open.feishu.cn","skillId":"lark-doc","name":"lark-doc","installs":436510,"weeklyInstalls":[39617,40320,42495,36649,44134,46563,39893,43803]}
	]`
	trendingFixtureSkills = `[
		{"source":"101-skills/skills","skillId":"ai-video-generation","name":"ai-video-generation","installs":21338},
		{"source":"vercel-labs/skills","skillId":"find-skills","name":"find-skills","installs":12742,"isOfficial":true}
	]`
	hotFixtureSkills = `[
		{"source":"getpaperclipai/paperclip","skillId":"design-guide","name":"design-guide","installs":489,"installsYesterday":485,"change":4},
		{"source":"claude-office-skills/skills","skillId":"asana-automation","name":"asana automation","installs":7,"installsYesterday":0,"change":7}
	]`
)

func TestParseLeaderboardDocumentCapturedShapesAndMultipleRSCChunks(t *testing.T) {
	tests := []struct {
		name       string
		view       CatalogView
		skills     string
		wantMetric int64
	}{
		{name: "all time", view: CatalogViewAllTime, skills: allTimeFixtureSkills, wantMetric: 2580013},
		{name: "trending 24h", view: CatalogViewTrending, skills: trendingFixtureSkills, wantMetric: 21338},
		{name: "hot", view: CatalogViewHot, skills: hotFixtureSkills, wantMetric: 489},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := rscLeaderboardDocument(t, leaderboardEnvelope(test.view, test.skills), 31)
			leaderboard, allTimeTotal, err := parseLeaderboardDocument(document, test.view, 2)
			if err != nil {
				t.Fatal(err)
			}
			if leaderboard.View != test.view || leaderboard.TotalSkills != 9576 || allTimeTotal != 946763 {
				t.Fatalf("leaderboard metadata = %#v, allTimeTotal = %d", leaderboard, allTimeTotal)
			}
			if len(leaderboard.Skills) != 2 || leaderboard.Skills[0].Rank != 1 || leaderboard.Skills[1].Rank != 2 {
				t.Fatalf("ranked Skills = %#v", leaderboard.Skills)
			}
			first := leaderboard.Skills[0]
			switch test.view {
			case CatalogViewAllTime:
				if first.TotalInstalls == nil || *first.TotalInstalls != test.wantMetric {
					t.Fatalf("all-time metric = %#v", first)
				}
			case CatalogViewTrending:
				if first.Installs24h == nil || *first.Installs24h != test.wantMetric {
					t.Fatalf("trending metric = %#v", first)
				}
			case CatalogViewHot:
				if first.CurrentInstalls == nil || *first.CurrentInstalls != test.wantMetric || first.YesterdayInstalls == nil || first.Change == nil {
					t.Fatalf("hot metrics = %#v", first)
				}
			}
		})
	}
}

func TestParseLeaderboardDocumentKeepsRealDomainIdentityBrowseableButNotInstallable(t *testing.T) {
	document := rscLeaderboardDocument(t, leaderboardEnvelope(CatalogViewAllTime, allTimeFixtureSkills))
	leaderboard, _, err := parseLeaderboardDocument(document, CatalogViewAllTime, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !leaderboard.Skills[0].Installable {
		t.Fatal("validated GitHub repository identity was not installable")
	}
	domain := leaderboard.Skills[1]
	if domain.Source != "open.feishu.cn" || domain.Installable {
		t.Fatalf("domain ranking identity = %#v", domain)
	}
}

func TestParseLeaderboardDocumentKeepsRealColonSkillIDBrowseableButNotInstallable(t *testing.T) {
	skills := `[{"source":"google-labs-code/stitch-skills","skillId":"react:components","name":"react:components","installs":20,"weeklyInstalls":[1,1,1,1,1,1,1,1]}]`
	document := rscLeaderboardDocument(t, leaderboardEnvelope(CatalogViewAllTime, skills))
	leaderboard, _, err := parseLeaderboardDocument(document, CatalogViewAllTime, 1)
	if err != nil {
		t.Fatal(err)
	}
	if leaderboard.Skills[0].SkillID != "react:components" || leaderboard.Skills[0].Installable {
		t.Fatalf("colon Skill identity = %#v", leaderboard.Skills[0])
	}
}

func TestParseLeaderboardDocumentRejectsDuplicateAndInvalidIdentities(t *testing.T) {
	tests := []struct {
		name   string
		skills string
		want   string
	}{
		{
			name: "duplicate source and Skill ID",
			skills: `[
				{"source":"acme/skills","skillId":"one","name":"one","installs":20},
				{"source":"acme/skills","skillId":"one","name":"one","installs":19}
			]`,
			want: "duplicate",
		},
		{
			name:   "invalid source",
			skills: `[{"source":"acme/skills;bad","skillId":"one","name":"one","installs":20}]`,
			want:   "source is invalid",
		},
		{
			name:   "invalid Skill ID",
			skills: `[{"source":"acme/skills","skillId":"bad;id","name":"bad id","installs":20}]`,
			want:   "Skill ID is invalid",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := rscLeaderboardDocument(t, leaderboardEnvelope(CatalogViewTrending, test.skills))
			_, _, err := parseLeaderboardDocument(document, CatalogViewTrending, 30)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseLeaderboardDocumentRejectsInvalidMetricsAndUpstreamShapeMismatch(t *testing.T) {
	tests := []struct {
		name     string
		view     CatalogView
		envelope string
		want     string
	}{
		{
			name: "fractional metric",
			view: CatalogViewTrending,
			envelope: leaderboardEnvelope(CatalogViewTrending,
				`[{"source":"acme/skills","skillId":"one","name":"one","installs":1.5}]`),
			want: "malformed",
		},
		{
			name: "negative metric",
			view: CatalogViewTrending,
			envelope: leaderboardEnvelope(CatalogViewTrending,
				`[{"source":"acme/skills","skillId":"one","name":"one","installs":-1}]`),
			want: "metric is invalid",
		},
		{
			name: "hot arithmetic mismatch",
			view: CatalogViewHot,
			envelope: leaderboardEnvelope(CatalogViewHot,
				`[{"source":"acme/skills","skillId":"one","name":"one","installs":8,"installsYesterday":3,"change":4}]`),
			want: "metrics are invalid",
		},
		{
			name: "wrong view identity",
			view: CatalogViewAllTime,
			envelope: leaderboardEnvelope(CatalogViewTrending,
				trendingFixtureSkills),
			want: "does not match",
		},
		{
			name: "unknown candidate field",
			view: CatalogViewTrending,
			envelope: leaderboardEnvelope(CatalogViewTrending,
				`[{"source":"acme/skills","skillId":"one","name":"one","installs":8,"score":99}]`),
			want: "changed shape",
		},
		{
			name: "unknown envelope field",
			view: CatalogViewTrending,
			envelope: `{"initialSkills":` + trendingFixtureSkills +
				`,"totalSkills":9576,"allTimeTotal":946763,"view":"trending","fallback":true}`,
			want: "changed shape",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := rscLeaderboardDocument(t, test.envelope)
			_, _, err := parseLeaderboardDocument(document, test.view, 30)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseLeaderboardDocumentRejectsMalformedOversizedAndExcessivePayloads(t *testing.T) {
	for name, document := range map[string][]byte{
		"visible HTML only": []byte(`<html><div data-initialSkills="[]">manufactured rows</div></html>`),
		"malformed frame":   []byte(`<script>self.__next_f.push([1,)</script>`),
		"missing envelope":  rscLeaderboardDocument(t, `{"other":[]}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseLeaderboardDocument(document, CatalogViewTrending, 30); err == nil {
				t.Fatal("malformed document was accepted")
			}
		})
	}

	oversizedChunk := rscLeaderboardDocument(t, strings.Repeat("x", MaxLeaderboardRSCBytes+1))
	if _, _, err := parseLeaderboardDocument(oversizedChunk, CatalogViewTrending, 30); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized RSC error = %v", err)
	}

	rows := make([]string, 0, MaxUpstreamLeaderboardRows+1)
	for index := 0; index <= MaxUpstreamLeaderboardRows; index++ {
		rows = append(rows, fmt.Sprintf(`{"source":"acme/skills","skillId":"skill-%d","name":"skill-%d","installs":%d}`, index, index, MaxUpstreamLeaderboardRows-index+1))
	}
	document := rscLeaderboardDocument(t, leaderboardEnvelope(CatalogViewTrending, `[`+strings.Join(rows, ",")+`]`))
	if _, _, err := parseLeaderboardDocument(document, CatalogViewTrending, 30); err == nil || !strings.Contains(err.Error(), "row count") {
		t.Fatalf("excessive row error = %v", err)
	}
}

func TestLeaderboardReaderFetchesAllThreeExactPagesAndFailsClosed(t *testing.T) {
	var mu sync.Mutex
	requested := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requested[request.URL.Path]++
		mu.Unlock()
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		var view CatalogView
		var skills string
		switch request.URL.Path {
		case "/":
			view, skills = CatalogViewAllTime, allTimeFixtureSkills
		case "/trending":
			view, skills = CatalogViewTrending, trendingFixtureSkills
		case "/hot":
			view, skills = CatalogViewHot, hotFixtureSkills
		default:
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write(rscLeaderboardDocument(t, leaderboardEnvelope(view, skills)))
	}))
	defer server.Close()

	reader := &LeaderboardReader{Client: server.Client(), BaseURL: server.URL, Timeout: time.Second}
	result, err := reader.Read(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AllTime.Skills) != 1 || len(result.Trending.Skills) != 1 || len(result.Hot.Skills) != 1 {
		t.Fatalf("bounded leaderboards = %#v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{"/", "/trending", "/hot"} {
		if requested[path] != 1 {
			t.Fatalf("requests[%q] = %d, want 1", path, requested[path])
		}
	}
}

func TestLeaderboardReaderRejectsRedirectNonHTMLNon200AndOversizedBodies(t *testing.T) {
	tests := []struct {
		name   string
		handle func(http.ResponseWriter, *http.Request)
		want   string
	}{
		{
			name: "redirect",
			handle: func(writer http.ResponseWriter, request *http.Request) {
				http.Redirect(writer, request, "/final", http.StatusFound)
			},
			want: "HTTP 302",
		},
		{
			name: "non HTML",
			handle: func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(`{}`))
			},
			want: "unexpected content",
		},
		{
			name: "non 200",
			handle: func(writer http.ResponseWriter, _ *http.Request) {
				http.Error(writer, "nope", http.StatusBadGateway)
			},
			want: "HTTP 502",
		},
		{
			name: "oversized body",
			handle: func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "text/html")
				_, _ = writer.Write([]byte(strings.Repeat("x", MaxLeaderboardBodyBytes+1)))
			},
			want: "body limit",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(test.handle))
			defer server.Close()
			reader := &LeaderboardReader{Client: server.Client(), BaseURL: server.URL, Timeout: time.Second}
			_, err := reader.Read(context.Background(), 1)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLeaderboardReaderTimeoutAndCancellationAreBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	reader := &LeaderboardReader{Client: server.Client(), BaseURL: server.URL, Timeout: 10 * time.Millisecond}
	if _, err := reader.Read(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader.Timeout = time.Second
	if _, err := reader.Read(ctx, 1); err != context.Canceled {
		t.Fatalf("cancellation error = %v, want context.Canceled", err)
	}
}

func leaderboardEnvelope(view CatalogView, skills string) string {
	return fmt.Sprintf(`{"initialSkills":%s,"totalSkills":9576,"allTimeTotal":946763,"view":%q}`, skills, view)
}

func rscLeaderboardDocument(t *testing.T, payload string, splitAt ...int) []byte {
	t.Helper()
	chunks := []string{`4a:["$","$L51",null,` + payload + "]\n"}
	if len(splitAt) > 0 && splitAt[0] > 0 && splitAt[0] < len(chunks[0]) {
		chunks = []string{chunks[0][:splitAt[0]], chunks[0][splitAt[0]:]}
	}
	var document strings.Builder
	document.WriteString(`<html><body><ol><li>visible rows are not parser input</li></ol>`)
	document.WriteString(rscFrame(t, []any{0}))
	for _, chunk := range chunks {
		document.WriteString(rscFrame(t, []any{1, chunk}))
	}
	document.WriteString(`</body></html>`)
	return []byte(document.String())
}

func rscFrame(t *testing.T, frame []any) string {
	t.Helper()
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	return `<script>self.__next_f.push(` + string(raw) + `)</script>`
}
