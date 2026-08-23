import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = readFileSync(
  join(import.meta.dir, "../app/settings.tsx"),
  "utf8",
);

describe("Settings copy density", () => {
  test("does not restate nearby controls, selection state, or screen identity", () => {
    for (const redundantCopy of [
      "Pair your first server below",
      "Pair via link or QR code",
      "System follows this device. Light and Dark stay fixed until changed.",
      "Mobile-native agent control plane",
      '"Current daemon"',
      "Scan the QR, or choose an image from this device.",
    ]) {
      expect(source).not.toContain(redundantCopy);
    }
  });

  test("retains requirements, consequences, security boundaries, and accessibility semantics", () => {
    for (const necessaryCopy of [
      "Camera permission required",
      "Allow camera access to scan a zen pairing QR code.",
      "Scan the one-time QR from zen pair, or paste its pairing link.",
      "Full-origin endpoint from LAN, Tailscale, Cloudflare",
      "Bot chats are Telegram cloud chats. The token remains on this daemon.",
      "Telegram cloud messages are not deleted.",
      'accessibilityRole="radiogroup"',
      'accessibilityRole="radio"',
      "accessibilityState={{ checked: selected }}",
    ]) {
      expect(source).toContain(necessaryCopy);
    }
  });
});
