package work

import "testing"

func TestFilterCalendarWorkItemsKeepsCalendarActionsOnly(t *testing.T) {
	items := []*Item{
		nil,
		{Frontmatter: Frontmatter{ID: "other", Kind: "task"}},
		{Frontmatter: Frontmatter{ID: "calendar", Kind: " calendar_action "}},
	}

	filtered := FilterCalendarWorkItems(items)
	if len(filtered) != 1 || filtered[0].Frontmatter.ID != "calendar" {
		t.Fatalf("filtered = %#v, want only Calendar action", filtered)
	}
	if IsCalendarWorkItem(nil) || IsCalendarWorkItem(items[1]) || !IsCalendarWorkItem(items[2]) {
		t.Fatalf("Calendar predicate disagrees with filtered items")
	}
}
