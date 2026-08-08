import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

describe("useSessionProviderSheet projection lifecycle", () => {
  const sheet = source("./screen/useSessionProviderSheet.ts");

  test("clears the projection on every server/agent rebind, even closed", () => {
    expect(sheet).toContain(
      "// Clear on every identity rebind, even while the sheet stays closed:",
    );
    expect(sheet).toContain("const rebound = ownerRef.current.rebind(");
    expect(sheet).toMatch(/if \(rebound\) \{\n\s+clearProjection\(\);/);
    expect(sheet).not.toMatch(/if \(rebound && visible\)/);
  });

  test("close keeps the projection so the Composer chip stays stable", () => {
    expect(sheet).toContain(
      "// Keep the loaded projection so the Composer model control stays stable",
    );
    expect(sheet).toContain("const close = useCallback(() => {");
    expect(sheet).not.toMatch(/setVisible\(false\);\n\s+clearProjection\(\);/);
  });

  test("eager load fetches once per server/agent epoch", () => {
    expect(sheet).toContain("eagerLoad?: boolean;");
    expect(sheet).toContain("const fetchedEpochRef = useRef<");
    expect(sheet).toContain(
      "if (fetched && fetched.serverId === serverId && fetched.agentId === agentId) {",
    );
    expect(sheet).toContain('void fetchProjection("eager");');
    expect(sheet).toContain('void fetchProjection("sheet");');
  });

  test("durable activation updates the selection and closes the sheet", () => {
    expect(sheet).toContain("setSelection(result.selection);");
    expect(sheet).toContain("fetchedEpochRef.current = { serverId, agentId };");
    expect(sheet).toContain('if (classification === "applied_durable") {');
    expect(sheet).toContain("setVisible(false);");
  });

  test("the Composer chip derives from the trusted current projection", () => {
    expect(sheet).toContain("resolveComposerModelControl({");
    expect(sheet).toContain("refreshRequired: requiresRefreshBeforeMutation,");
    expect(sheet).toContain("composerControl,");
  });
});
