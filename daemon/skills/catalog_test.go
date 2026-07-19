package skills

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCatalogSearchBoundsAndValidatesUntrustedResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.URL.Query().Get("q"); got != "react native" {
			t.Errorf("query = %q", got)
		}
		if got := request.URL.Query().Get("limit"); got != "2" {
			t.Errorf("limit = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"skills":[
			{"id":"acme/skills/react-native","name":"react-native","installs":12,"source":"acme/skills"},
			{"id":"acme/skills/evil;name","name":"evil;name","installs":4,"source":"acme/skills"},
			{"id":"vercel-labs/agent-skills/design","name":"design","installs":8,"source":"vercel-labs/agent-skills"}
		]}`))
	}))
	defer server.Close()

	searcher := &Searcher{Client: server.Client(), Endpoint: server.URL, Timeout: time.Second}
	result, err := searcher.Search(context.Background(), " react native ", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skills) != 2 || result.Skills[0].ID != "acme/skills/react-native" || result.Skills[1].ID != "vercel-labs/agent-skills/design" {
		t.Fatalf("skills = %#v", result.Skills)
	}
}

func TestCatalogSearchRejectsAmbiguousDuplicateIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"skills":[
			{"id":"acme/skills/design","name":"design","installs":1,"source":"acme/skills"},
			{"id":"acme/skills/design","name":"design","installs":2,"source":"acme/skills"}
		]}`))
	}))
	defer server.Close()
	searcher := &Searcher{Client: server.Client(), Endpoint: server.URL, Timeout: time.Second}
	if _, err := searcher.Search(context.Background(), "design", 10); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %v, want ambiguous duplicate rejection", err)
	}
}

func TestCatalogSearchValidatesAmbiguityBeyondReturnedLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"skills":[
			{"id":"acme/skills/first","name":"first","installs":1,"source":"acme/skills"},
			{"id":"acme/skills/later","name":"later","installs":1,"source":"acme/skills"},
			{"id":"acme/skills/later","name":"later","installs":2,"source":"acme/skills"}
		]}`))
	}))
	defer server.Close()
	searcher := &Searcher{Client: server.Client(), Endpoint: server.URL, Timeout: time.Second}
	if _, err := searcher.Search(context.Background(), "design", 1); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %v, want ambiguity beyond result limit rejected", err)
	}
}

func TestCatalogSearchRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"skills":[] ,"padding":"` + strings.Repeat("x", MaxCatalogBodyBytes) + `"}`))
	}))
	defer server.Close()
	searcher := &Searcher{Client: server.Client(), Endpoint: server.URL, Timeout: time.Second}
	if _, err := searcher.Search(context.Background(), "design", 10); err == nil || !strings.Contains(err.Error(), "body limit") {
		t.Fatalf("error = %v, want body limit", err)
	}
}

func TestCatalogSearchTimeoutAndFailureAreExplicit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(80 * time.Millisecond)
		_, _ = writer.Write([]byte(`{"skills":[]}`))
	}))
	defer server.Close()
	searcher := &Searcher{Client: server.Client(), Endpoint: server.URL, Timeout: 10 * time.Millisecond}
	if _, err := searcher.Search(context.Background(), "design", 10); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}

	failure := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "nope", http.StatusBadGateway)
	}))
	defer failure.Close()
	searcher = &Searcher{Client: failure.Client(), Endpoint: failure.URL, Timeout: time.Second}
	if _, err := searcher.Search(context.Background(), "design", 10); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("error = %v, want HTTP failure", err)
	}
}

func TestCatalogSearchRejectsEveryRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/final" {
			_, _ = writer.Write([]byte(`{"skills":[]}`))
			return
		}
		http.Redirect(writer, request, "/final", http.StatusFound)
	}))
	defer server.Close()
	client := server.Client()
	searcher := &Searcher{Client: client, Endpoint: server.URL, Timeout: time.Second}
	if _, err := searcher.Search(context.Background(), "design", 10); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("redirect error = %v", err)
	}
}
