// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = readFileSync(join(import.meta.dir, "../app/onboarding.tsx"), "utf8");

describe("first-run onboarding guidance", () => {
  test("keeps the reachable private-network flow and immediate CTA", () => {
    expect(source).toContain("Zen runs agents on your computer");
    expect(source).toContain("zen --lan");
    expect(source).toContain("zen pair http://192.168.1.42:9876");
    expect(source).toContain("same trusted Wi-Fi");
    expect(source).toContain("Scan or import pairing code");
    expect(source).toContain('pathname: "/settings"');
    expect(source).toContain('pairingRequired: "1"');
  });

  test("does not regress to unreachable or wildcard pairing commands", () => {
    expect(source).not.toContain("<Text style={styles.code}>zen</Text>");
    expect(source).not.toContain("zen pair http://0.0.0.0");
    expect(source).not.toContain("zen pair https://your-host.example");
  });
});
