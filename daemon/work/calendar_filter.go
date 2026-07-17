package work

import "strings"

func IsCalendarWorkItem(item *Item) bool {
	return item != nil && strings.TrimSpace(item.Frontmatter.Kind) == "calendar_action"
}

func FilterCalendarWorkItems(items []*Item) []*Item {
	if len(items) == 0 {
		return nil
	}
	out := make([]*Item, 0, len(items))
	for _, item := range items {
		if IsCalendarWorkItem(item) {
			out = append(out, item)
		}
	}
	return out
}
