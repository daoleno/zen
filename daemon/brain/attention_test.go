package brain

import (
	"reflect"
	"testing"
	"time"
)

func TestSortAttentionQueueOrdersSerialAttentionWork(t *testing.T) {
	base := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	items := []AttentionQueueItem{
		{ID: "thread:review", Kind: "review_thread", Updated: base.Add(5 * time.Hour)},
		{ID: "work:blocked", Kind: "work_item", Status: "blocked", Updated: base.Add(4 * time.Hour)},
		{ID: "work:failed-old", Kind: "work_item", Status: "failed", Updated: base.Add(1 * time.Hour)},
		{ID: "work:failed-new", Kind: "work_item", Status: "failed", Updated: base.Add(2 * time.Hour)},
		{ID: "agent:blocked", Kind: "blocked_agent", Updated: base},
	}

	SortAttentionQueue(items)

	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.ID)
	}
	want := []string{
		"agent:blocked",
		"work:failed-new",
		"work:failed-old",
		"work:blocked",
		"thread:review",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("queue order = %#v, want %#v", got, want)
	}
}
