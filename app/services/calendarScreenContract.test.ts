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
  test("keeps a compact, separate Calendar hierarchy", () => {
    expect(source).toContain('accessibilityLabel="Back"');
    expect(source).toContain('accessibilityLabel="Add calendar item"');
    expect(source).toContain("styles.appBar");
    expect(source).toContain("styles.modeBar");
    expect(source).toContain("<AgendaHeading now={now} timeZone={viewerZone} />");
    expect(source).toMatch(/appBar:\s*\{[^}]*height: 52/s);
    expect(source).toMatch(/appBarAction:\s*\{[^}]*width: 44,[^}]*height: 44/s);
    expect(source).toMatch(/modeBar:\s*\{[^}]*paddingHorizontal: Spacing\.lg/s);
    expect(source).toMatch(/segmentButton:\s*\{[^}]*minHeight: 36/s);
  });
  test("anchors concise Agenda states below a localized Today time anchor", () => {
    expect(source).toContain("timeZoneName: \"short\"");
    expect(source).toContain("Nothing planned");
    expect(source).toContain("Add item");
    expect(source).toContain("Loading calendar…");
    expect(source).toContain("Nothing planned for this day.");
    expect(source).not.toContain("Your time is clear");
    expect(source).not.toContain("Commitments created by you or Brain");
    expect(source).not.toContain("Create item");
    expect(source).toMatch(/empty:\s*\{[^}]*minHeight: 112/s);
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
  test("keeps single-line and multiline field text inside distinct geometry", () => {
    const field = source.slice(
      source.indexOf("const Field = React.memo"),
      source.indexOf("function AmbiguityChoice"),
    );
    const styles = source.slice(source.indexOf("function createStyles"));

    expect(field).toContain(
      "input.multiline ? styles.multiline : styles.singleLine",
    );
    expect(styles).toMatch(
      /singleLine:\s*\{[^}]*height: 48,[^}]*paddingVertical: 0,[^}]*textAlignVertical: "center"/s,
    );
    expect(styles).toMatch(
      /multiline:\s*\{[^}]*minHeight: 92,[^}]*paddingVertical: 10,[^}]*textAlignVertical: "top"/s,
    );
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
