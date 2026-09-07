import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = readFileSync(join(import.meta.dir, "../app/onboarding.tsx"), "utf8");
const presentation = readFileSync(join(import.meta.dir, "../components/onboarding/OnboardingPresentation.tsx"), "utf8");
const settings = readFileSync(join(import.meta.dir, "../app/settings.tsx"), "utf8");
const importer = readFileSync(join(import.meta.dir, "importConnection.ts"), "utf8");

describe("scan-first onboarding", () => {
  test("prioritizes scan and import, with collapsed computer setup", () => {
    expect(presentation).toContain('useState(false)');
    expect(presentation).toContain('onPair("scanner")');
    expect(presentation).toContain('onPair("editor")');
    expect(presentation).toContain("accessibilityState={{ expanded: setup }}");
    expect(presentation.indexOf('accessibilityLabel="Scan pairing code"')).toBeLessThan(presentation.indexOf('accessibilityLabel="Computer setup"'));
  });
  test("uses supported commands without inventing a pairing origin", () => {
    expect(presentation).toContain('command: "zen doctor"');
    expect(presentation).toContain('command: "zen --lan"');
    expect(presentation).toContain("pairing command printed by Zen");
    expect(presentation).not.toMatch(/192\.168|0\.0\.0\.0|zen pair http/);
    expect(presentation).toContain("install-daemon.md");
    expect(presentation).toContain("connect-and-pair.md");
  });
  test("shares enrollment pipeline without completion on UI dismissal", () => {
    expect(source).toContain('pairingRequired: "1", pairMode: mode');
    expect(settings).toContain('if (params.pairMode === "scanner") openScanner()');
    expect(settings).toContain('pathname: "/onboarding", params: { paired: "1" }');
    expect(source).not.toContain("markOnboarded");
    expect(importer.indexOf("await markOnboarded()")).toBeGreaterThan(importer.indexOf("await saveServer("));
    expect(importer.indexOf("await saveServer(")).toBeGreaterThan(importer.indexOf("await enrollWithDaemon("));
  });
  test("paired flow reports connection progress and current-server recovery", () => {
    expect(source).toContain("isCurrentServer(server.id)");
    expect(presentation).toContain('label: "Retry connection"');
    expect(presentation).toContain('label: "Open Brain"');
    expect(presentation).toContain("busy={connecting}");
  });
});
