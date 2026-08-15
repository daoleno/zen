// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const barSource = readFileSync(
  join(import.meta.dir, "AgentSessionSelectionBar.tsx"),
  "utf8",
);
const rowSource = readFileSync(
  join(import.meta.dir, "AgentSessionRow.tsx"),
  "utf8",
);
const containerSource = readFileSync(
  join(import.meta.dir, "AgentListRowContainer.tsx"),
  "utf8",
);
const listSource = readFileSync(
  join(import.meta.dir, "../../app/(primary)/list.tsx"),
  "utf8",
);

describe("AgentSessionSelectionBar chrome", () => {
  test("offers Cancel, selected count and destructive Terminate", () => {
    expect(barSource).toContain(">Cancel</Text>");
    expect(barSource).toContain("selectionCountLabel(count)");
    expect(barSource).toContain('{terminating ? "Terminating…" : "Terminate"}');
  });

  test("uses the destructive danger token and disables while terminating", () => {
    expect(barSource).toContain("colors.dangerText");
    expect(barSource).toContain("terminateDisabled && styles.terminateTextDisabled");
    expect(barSource).toContain("disabled={cancelDisabled}");
    expect(barSource).toContain("disabled={terminateDisabled}");
    expect(barSource).toContain("busy: terminating");
  });

  test("keeps the count truthful for screen readers", () => {
    expect(barSource).toContain('accessibilityLabel="Cancel selection"');
    expect(barSource).toContain('accessibilityLabel="Terminate selected sessions"');
    expect(barSource).toContain("accessibilityLiveRegion");
  });
});

describe("AgentSessionRow selection contract", () => {
  test("long press stays installed; tap toggles in selection mode", () => {
    expect(rowSource).toContain("onLongPress={onLongPress}");
    expect(rowSource).toContain("onPress={inSelectionMode ? onToggleSelection : onPress}");
  });

  test("keeps the long-press handler installed through press release", () => {
    expect(rowSource).toContain("onLongPress={onLongPress}");
    expect(containerSource).toContain("if (selectionMode)");
    expect(containerSource).toContain("return;");
  });

  test("becomes a checkbox in selection mode and stays a button otherwise", () => {
    expect(rowSource).toContain("accessibilityRole={inSelectionMode ? 'checkbox' : 'button'}");
    expect(rowSource).toContain("checked: inSelectionMode ? selected : undefined");
    expect(rowSource).toContain("'Double tap to toggle selection'");
  });

  test("renders a clear selected/unselected check state", () => {
    expect(rowSource).toContain("'checkmark-circle'");
    expect(rowSource).toContain("'ellipse-outline'");
    expect(rowSource).toContain("name={selected ? 'checkmark-circle' : 'ellipse-outline'}");
    expect(rowSource).toContain(
      "(activelyRunning || (inSelectionMode && selected)) && styles.rowActive",
    );
  });

  test("ineligible rows are disabled inside selection mode only", () => {
    expect(rowSource).toContain("const rowDisabled = inSelectionMode && selectionDisabled;");
    expect(rowSource).toContain("disabled={rowDisabled}");
    expect(rowSource).toContain("disabled: rowDisabled");
  });
});

describe("AgentListRowContainer selection contract", () => {
  test("long press enters selection instead of opening a context menu", () => {
    expect(containerSource).toContain("onEnterSelection(agent)");
    expect(containerSource).not.toContain("onOpenContextMenu");
    expect(containerSource).not.toContain("openContextMenu");
  });

  test("press opens the Session in normal mode and toggles in selection mode", () => {
    expect(containerSource).toContain("onOpenAgent(agent)");
    expect(containerSource).toContain("onToggleSelection(agent)");
    expect(containerSource).toContain("if (selectionMode)");
  });
});

describe("Sessions list integration contract", () => {
  test("long press entry, toggle, cancel and back are wired", () => {
    expect(listSource).toContain("enterSelectionMode");
    expect(listSource).toContain("toggleSessionSelection(current, agent.key)");
    expect(listSource).toContain("exitSelectionMode");
    expect(listSource).toContain("usePrimaryDrawerBack");
  });

  test("selection survives reorder by stable key and prunes on disappearance", () => {
    expect(listSource).toContain("pruneSessionSelection");
    expect(listSource).toContain("authoritativeAgentKeySet");
    expect(listSource).toContain("batch.settleDisappeared");
  });

  test("batch submission uses authoritative selected Sessions, not visible rows", () => {
    expect(listSource).toContain(
      "state.agents.filter((agent) => selectedKeys.has(agent.key))",
    );
    expect(listSource).not.toContain(
      "sortedAgents.filter((agent) => selectedKeys.has(agent.key))",
    );
  });

  test("one confirmation with the exact count, exact IDs submitted", () => {
    expect(listSource).toContain("sessionTerminationConfirmMessage(count, serverCount)");
    expect(listSource).toContain("createSessionTerminationEntries");
    expect(listSource).toContain("serverId: agent.serverId");
    expect(listSource).toContain("agentId: agent.id");
  });

  test("duplicate submission is prevented while a batch runs", () => {
    expect(listSource).toContain("if (terminationRunning || selectedAgents.length === 0)");
    expect(listSource).toContain("if (!started)");
  });

  test("successes leave selection and failures stay selected with retry", () => {
    expect(listSource).toContain("removeSessionsFromSelection");
    expect(listSource).toContain("addSessionToSelection(next, entry.sessionKey)");
    expect(listSource).toContain("sessionTerminationSummaryMessage");
  });

  test("the row context menu long-press surface is gone", () => {
    expect(listSource).not.toContain("openContextMenu");
    expect(listSource).not.toContain("menuAgent");
    expect(listSource).not.toContain("handleTerminateAgent");
  });

  test("last deselection exits selection mode", () => {
    expect(listSource).toContain("countSessionSelection(selectedKeys) === 0");
    expect(listSource).toContain("exitSelectionMode()");
  });

  test("selection chrome replaces the app bar while active", () => {
    expect(listSource).toContain("usePrimarySelectionBar(selectionBar)");
    expect(listSource).toContain("<AgentSessionSelectionBar");
  });
});
