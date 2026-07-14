import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
const source = readFileSync(
  join(import.meta.dir, "../app/calendar.tsx"),
  "utf8",
);
describe("Calendar screen contract", () => {
  test("is agenda-first with month and day modes", () => {
    expect(source).toContain('props.initialMode ?? "agenda"');
    expect(source).toContain('["agenda", "month", "day"]');
    expect(source).toContain("groupAgenda");
  });
  test("exposes all kinds, lifecycle, execution and editing", () => {
    for (const value of [
      "event",
      "reminder",
      "deadline",
      "scheduled_action",
      "Run now",
      "Retry now",
      "Linked Work",
      "Action instruction",
      "Timezone",
      "Recurrence",
    ])
      expect(source).toContain(value);
  });
  test("has graceful notification denial and keyboard-safe editor", () => {
    expect(source).toContain("Reminder notifications are");
    expect(source).toContain("syncCalendarNotifications");
    expect(source).toContain("KeyboardAvoidingView");
    expect(source).toContain('Platform.OS === "ios" ? "padding" : "height"');
    expect(source).toContain("keyboardDismissMode=\"on-drag\"");
    expect(source).toContain("editorSessionId");
    expect(source).toContain("React.memo(function Field");
    expect(source).toContain("safe-area-context");
  });
  test("keeps one editor session through keyboard hide and second focus", () => {
    const editor = source.slice(
      source.indexOf("function EditorModal"),
      source.indexOf("const Field = React.memo"),
    );

    expect(editor).toContain("value === \"new\"");
    expect(editor).toContain("[editorSessionId]");
    expect(editor).not.toContain("Keyboard.addListener");
    expect(editor).not.toContain('height: "94%"');
    expect(editor).not.toContain("keyboardVisible");
  });
  test("keeps Calendar copy concise and action-oriented", () => {
    for (const copy of [
      "Grouped by your device timezone",
      "canonical Calendar state",
      "Calendar remains available",
      "Repeats preserve this wall-clock time",
    ]) {
      expect(source).not.toContain(copy);
    }
  });
});
