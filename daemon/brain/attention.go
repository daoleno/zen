package brain

import (
	"sort"
	"strings"
)

// SortAttentionQueue applies Brain's provider-neutral attention priority.
// Producers may append items from different sources; the merged queue should
// still spend the user's serial attention on blocked agents and failed work
// before ordinary review backlog.
func SortAttentionQueue(items []AttentionQueueItem) {
	sort.SliceStable(items, func(i, j int) bool {
		leftPriority := AttentionQueueItemPriority(items[i])
		rightPriority := AttentionQueueItemPriority(items[j])
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		if !items[i].Updated.Equal(items[j].Updated) {
			return items[i].Updated.After(items[j].Updated)
		}
		return items[i].ID < items[j].ID
	})
}

func AttentionQueueItemPriority(item AttentionQueueItem) int {
	switch strings.TrimSpace(item.Kind) {
	case "blocked_agent":
		return 0
	case "work_item":
		if strings.EqualFold(strings.TrimSpace(item.Status), "failed") {
			return 1
		}
		return 2
	case "review_thread":
		return 3
	default:
		return 9
	}
}
