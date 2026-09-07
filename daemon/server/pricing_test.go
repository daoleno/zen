package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/stats"
	"github.com/gorilla/websocket"
)

func TestPricingStatusSurvivesStatsWebSocketProjection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	collector := stats.NewCollector()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Publish empty local history without starting a pricing request.
	collector.Start(ctx)
	s := New(nil, nil, nil, collector, nil, nil, nil)
	upgrader := websocket.Upgrader{}
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		s.mu.Lock()
		s.writes[conn] = &sync.Mutex{}
		s.mu.Unlock()
		defer func() { s.mu.Lock(); delete(s.writes, conn); s.mu.Unlock() }()
		s.handleClientMessage(conn, []byte(`{"type":"get_stats","request_id":"pricing-test"}`))
	}))
	defer host.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(host.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(time.Second))
	var response struct {
		Type      string              `json:"type"`
		RequestID string              `json:"request_id"`
		Pricing   stats.PricingStatus `json:"pricing"`
	}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response.Type != "stats_data" || response.RequestID != "pricing-test" ||
		!strings.Contains(response.Pricing.Basis, "not actual gateway bills") {
		t.Fatalf("pricing projection lost: %+v", response)
	}
}
