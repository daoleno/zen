import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
const source = readFileSync(
  join(import.meta.dir, "../app/calendar.tsx"),
  "utf8",
);
describe("Calendar screen contract", () => {
  test("uses one canonical Agenda stream with an inline month navigator", () => {
    expect(source).toContain("groupAgenda");
    expect(source).toContain("monthExpanded");
    expect(source).toContain("selectedDate");
    expect(source).toContain("<MonthNavigator");
    expect(source).not.toContain("type Mode");
    expect(source).not.toContain("initialMode");
    expect(source).not.toContain("setMode");
    expect(source).not.toContain('"day"');
  });
  test("keeps a compact, separate Calendar hierarchy", () => {
    expect(source).toContain('accessibilityLabel="Back"');
    expect(source).toContain('accessibilityLabel="Add calendar item"');
    expect(source).toContain("headerShown: true");
    expect(source).toContain('edges={["bottom"]}');
    expect(source).not.toContain("styles.appBar");
    expect(source).not.toContain("styles.modeBar");
    expect(source).toContain('accessibilityLabel="Month date navigator"');
    expect(source).toContain('accessibilityLabel="Return to today"');
    expect(source).toMatch(/calendarHeaderAction:\s*\{[^}]*width: 44,[^}]*height: 44/s);
    expect(source).toContain('"Expand month calendar"');
    expect(source).toContain('"Collapse month calendar"');
    expect(source).not.toContain("segmentButton");
    expect(source).toContain("maxFontSizeMultiplier={1.15}");
  });
  test("anchors concise Agenda states below a localized Today time anchor", () => {
    expect(source).toContain("timeZoneName: \"short\"");
    expect(source).toContain("Nothing planned");
    expect(source).toContain("Add calendar item");
    expect(source).toContain("Loading calendar…");
    expect(source).toContain("Nothing planned for this date");
    expect(source).not.toContain("Your time is clear");
    expect(source).not.toContain("Commitments created by you or Brain");
    expect(source).not.toContain("Create item");
    expect(source).toMatch(/empty:\s*\{[^}]*minHeight: 52/s);
    expect(source).not.toContain("style={styles.primaryButton}");
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
