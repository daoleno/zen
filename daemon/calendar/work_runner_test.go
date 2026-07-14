package calendar

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/work"
)

type scheduledActionWatcher struct {
	agent *classifier.Agent
}

func (w scheduledActionWatcher) GetAgent(string) *classifier.Agent {
	return w.agent
}

func TestInspectScheduledActionReturnsCompleteStructuredDeliverable(t *testing.T) {
	store, err := work.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	done := time.Now().UTC()
	deliverable := `Three recent AI papers point to a shared shift: inference-time
reasoning is becoming a first-class scaling axis. The first paper shows that
verification can improve long-horizon answers without retraining the base model.

The second paper separates search quality from answer fluency. Its ablations
suggest that extra tokens help only when the search policy preserves diversity.

The third paper studies tool-using agents. It reports stronger factuality when
the agent records provenance and retries failed retrievals.

Taken together, the practical recommendation is to budget explicitly for
verification, preserve multiple candidates until late in the search, and keep
citations attached to intermediate evidence.`
	body := `# AI paper review

## Instructions

Summarize three recent AI papers for the user.

## User-facing deliverable

Executor-only guidance that must not be delivered.

` + scheduledDeliverableStart + "\n" + deliverable + "\n" + scheduledDeliverableEnd + `

## Calendar metadata

Calendar item: ` + "`calendar-1`" + `
Scheduled for: 2026-07-14 09:00:00 CST (Asia/Shanghai)
`
	created, err := store.Write(&work.Item{
		Path: t.TempDir() + "/unused.md",
		Body: body,
		Frontmatter: work.Frontmatter{
			ID:      "run-1",
			Kind:    "calendar_action",
			Created: done.Add(-time.Minute),
			Done:    &done,
			Outcome: "Truncated digest that must not win.",
		},
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	status, result, failure, known := (&WorkRunner{Store: store}).InspectScheduledAction(
		context.Background(),
		Item{},
		Run{WorkID: created.ID},
	)
	if !known || status != StatusCompleted || failure != "" {
		t.Fatalf("inspection = (%q, %q, %q, %v)", status, result, failure, known)
	}
	if result != deliverable {
		t.Fatalf("deliverable changed:\n--- got ---\n%s\n--- want ---\n%s", result, deliverable)
	}
	for _, excluded := range []string{"Executor-only guidance", "Calendar item:", "Scheduled for:", "Truncated digest"} {
		if strings.Contains(result, excluded) {
			t.Fatalf("result leaked scaffolding %q:\n%s", excluded, result)
		}
	}
}

func TestInspectScheduledActionFallsBackForLegacyWork(t *testing.T) {
	store, err := work.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.Write(&work.Item{
		Path: t.TempDir() + "/legacy.md",
		Body: "# Legacy scheduled action",
		Frontmatter: work.Frontmatter{
			ID:      "legacy-run",
			Created: time.Now().UTC(),
			Outcome: "Legacy outcome",
		},
	}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	runner := &WorkRunner{
		Store:   store,
		Watcher: scheduledActionWatcher{agent: &classifier.Agent{State: classifier.StateDone, Summary: "Classifier summary"}},
	}
	status, result, _, known := runner.InspectScheduledAction(context.Background(), Item{}, Run{WorkID: created.ID, AgentSession: "agent-1"})
	if !known || status != StatusCompleted || result != "Legacy outcome" {
		t.Fatalf("legacy inspection = (%q, %q, %v)", status, result, known)
	}
}

func TestScheduledWorkBodyRequiresBoundedStructuredDeliverable(t *testing.T) {
	due := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.UTC)
	body := scheduledWorkBody(
		Item{ID: "calendar-1", Title: "Paper review", ActionInstruction: "Summarize the papers", Timezone: "UTC"},
		Run{ScheduledFor: due},
	)
	for _, want := range []string{
		"Write the complete result for the user between the markers below",
		"do not put execution notes or Calendar metadata in this section",
		"at or below 256 KiB",
		scheduledDeliverableStart,
		scheduledDeliverablePlaceholder,
		scheduledDeliverableEnd,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("generated Work body missing %q:\n%s", want, body)
		}
	}
}

func TestExtractScheduledDeliverableUsesUTF8SafeSizeBound(t *testing.T) {
	oversized := strings.Repeat("界", maxScheduledDeliverableBytes)
	got, ok := extractScheduledDeliverable(scheduledDeliverableStart + oversized + scheduledDeliverableEnd)
	if !ok || len(got) > maxScheduledDeliverableBytes || !strings.HasSuffix(got, "界") {
		t.Fatalf("bounded result bytes=%d ok=%v", len(got), ok)
	}
}
