// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = readFileSync(join(import.meta.dir, "../app/onboarding.tsx"), "utf8");

describe("first-run onboarding", () => {
  test("shares the product story and complete shortest LAN path", () => {
    expect(source).toContain("Your coding agents, wherever you are.");
    expect(source).toContain("code and");
    expect(source).toContain("credentials stay on your computer");
    expect(source).toContain('command: "zen doctor"');
    expect(source).toContain('command: "zen --lan"');
    expect(source).toContain("Run the pair command Zen prints");
    expect(source).toContain("Scan or import pairing code");
  });

  test("keeps pairing primary and remote setup secondary", () => {
    expect(source).toContain('pathname: "/settings"');
    expect(source).toContain('pairingRequired: "1"');
    expect(source).toContain("Remote HTTPS connection guide");
    expect(source).toContain("docs/connect-and-pair.md");
  });

  test("does not suggest an unreachable pairing origin or extra first-run routes", () => {
    expect(source).not.toContain("zen pair http://0.0.0.0");
    expect(source).not.toContain("zen pair https://your-host.example");
    expect(source).not.toContain("Cloudflare");
    expect(source).not.toContain("Funnel");
  });

  test("keeps the primary CTA outside the scrollable instructions", () => {
    expect(source.indexOf("</ScrollView>")).toBeLessThan(
      source.indexOf("style={styles.actionArea}"),
    );
  });
});
