import { describe, expect, test } from "bun:test";
import { sessionEmptyState } from "./sessionEmptyState";
import { unpricedReasonLabel } from "./unpricedReason";
import { readFileSync } from "node:fs";
import { join } from "node:path";

describe("empty-state actions", () => {
  test("state determines the action, not the existence of other servers", () => {
    expect(sessionEmptyState(false, undefined).action).toBe("pair");
    expect(sessionEmptyState(true, "connecting")).toMatchObject({ action: null, busy: true });
    expect(sessionEmptyState(true, "offline").action).toBe("retry");
    expect(sessionEmptyState(true, "connected").action).toBe("terminal");
    expect(sessionEmptyState(true, "connected", true).action).toBe("clear");
    expect(sessionEmptyState(true, "offline", true).action).toBe("retry");
  });
  test("existing skill filters reset both search and filter state", () => {
    const source = readFileSync(join(import.meta.dir, "../components/skills/SkillsPresentation.tsx"), "utf8");
    expect(source).toContain('setQuery(""); setFilters(DEFAULT_FILTERS)');
    expect(source).toContain('action="Clear filters"');
  });
  test("primary actions have accessible names, disabled state, and a minimum touch target", () => {
    const source = readFileSync(join(import.meta.dir, "../components/ui/CompactEmptyState.tsx"), "utf8");
    expect(source).toContain('accessibilityRole="button"');
    expect(source).toContain("accessibilityLabel={action.label}");
    expect(source).toContain("disabled: Boolean(action.disabled)");
    expect(source).toContain("minHeight: 48");
  });
  test("model cost states distinguish catalog discovery from missing context", () => {
    expect(unpricedReasonLabel("missing_model")).toBe("Price not in catalog");
    expect(unpricedReasonLabel("insufficient_context")).toBe("Price found; request context unavailable");
    expect(unpricedReasonLabel("missing_rate")).toBe("Some token rates unavailable");
    expect(unpricedReasonLabel(undefined)).toBeNull();
  });
});
