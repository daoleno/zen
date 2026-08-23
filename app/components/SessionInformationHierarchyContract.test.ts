import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const componentRoot = import.meta.dir;
const rowSource = readFileSync(
  join(componentRoot, "agents/AgentSessionRow.tsx"),
  "utf8",
);
const rowContainerSource = readFileSync(
  join(componentRoot, "agents/AgentListRowContainer.tsx"),
  "utf8",
);
const sheetSource = readFileSync(
  join(componentRoot, "terminal/SessionResourceSheet.tsx"),
  "utf8",
);
const overlaysSource = readFileSync(
  join(componentRoot, "terminal/screen/TerminalScreenOverlays.tsx"),
  "utf8",
);
const overlayPropsSource = readFileSync(
  join(componentRoot, "terminal/screen/useTerminalScreenOverlayProps.ts"),
  "utf8",
);
const layoutPropsSource = readFileSync(
  join(componentRoot, "terminal/screen/useTerminalScreenLayoutProps.ts"),
  "utf8",
);

describe("Session list information hierarchy", () => {
  test("keeps status words out of the row while preserving one hidden indicator", () => {
    expect(rowSource).not.toContain("agentStatusLabel");
    expect(rowSource).not.toContain("statusText");
    expect(rowSource).not.toContain("statusDot");
    expect(rowSource).toContain("agentStatusIndicatorIcon(status)");

    const indicator = rowSource.slice(
      rowSource.indexOf("style={styles.statusIndicator}"),
      rowSource.indexOf(
        "</View>",
        rowSource.indexOf("style={styles.statusIndicator}"),
      ),
    );
    expect(indicator).toContain("accessibilityElementsHidden");
    expect(indicator).toContain(
      'importantForAccessibility="no-hide-descendants"',
    );
  });

  test("attaches delegated Brain origin to the leading icon, not the title", () => {
    expect(rowContainerSource).toContain(
      "brainDelegated={Boolean(agent.delegated)}",
    );
    expect(rowSource).not.toContain("showBrainBadge");
    expect(rowSource).not.toContain(">Brain</Text>");

    const iconStart = rowSource.indexOf("<AgentKindIcon");
    const marker = rowSource.indexOf("styles.brainOriginMarker");
    const body = rowSource.indexOf("<View style={styles.body}>");
    expect(iconStart).toBeGreaterThan(-1);
    expect(marker).toBeGreaterThan(iconStart);
    expect(marker).toBeLessThan(body);
    expect(rowSource.slice(marker, body)).toContain(
      "accessibilityElementsHidden",
    );
  });

  test("keeps the list title one line and frees it from fixed metadata width", () => {
    expect(rowSource).toContain(
      "<Text style={styles.title} numberOfLines={1}>",
    );
    expect(rowSource).not.toContain("minWidth: 60");
    expect(rowSource).not.toContain("maxWidth: 84");
  });
});

describe("Interface Session title ownership", () => {
  test("passes the exact resolved header title into the resource sheet", () => {
    expect(layoutPropsSource).toContain("title: headerTitle");
    expect(layoutPropsSource).toContain("resourceSheetTitle: headerTitle");
    expect(overlayPropsSource).toContain("resourceSheetTitle,");
    expect(overlaysSource).toContain("sessionTitle={resourceSheetTitle}");
  });

  test("renders the selectable full title before every resource state", () => {
    const scrollStart = sheetSource.indexOf("<ScrollView");
    const titleStart = sheetSource.indexOf('accessibilityRole="header"');
    const titleEnd = sheetSource.indexOf("</Text>", titleStart);
    const stateBranch = sheetSource.indexOf("{m ? (", titleEnd);
    const titleBlock = sheetSource.slice(titleStart, titleEnd);

    expect(scrollStart).toBeGreaterThan(-1);
    expect(titleStart).toBeGreaterThan(scrollStart);
    expect(titleEnd).toBeLessThan(stateBranch);
    expect(titleBlock).toContain("styles.sessionTitle");
    expect(titleBlock).toContain("selectable");
    expect(titleBlock).toContain("{sessionTitle}");
    expect(titleBlock).not.toContain("numberOfLines");
    expect(titleBlock).not.toContain("ellipsizeMode");
    expect(sheetSource).not.toContain("presentAgent");
  });
});
