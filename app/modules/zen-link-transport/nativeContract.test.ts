import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir);

describe("Zen Link native pin contract", () => {
  test("Android and iOS both pin TLS 1.3 SPKI and bind loopback only", () => {
    const android = readFileSync(
      join(
        root,
        "android/src/main/java/expo/modules/zenlinktransport/ZenLinkTransportModule.kt",
      ),
      "utf8",
    );
    const ios = readFileSync(
      join(root, "ios/ZenLinkTransportModule.swift"),
      "utf8",
    );

    for (const source of [android, ios]) {
      expect(source).toContain("127.0.0.1");
      expect(source).toContain("32 * 1024");
      expect(source).toContain("64");
    }
    expect(android).toContain("TLSv1.3");
    expect(ios).toContain(".TLSv13");
    expect(android).toContain("leaf.publicKey.encoded");
    expect(android).toContain('MessageDigest.getInstance("SHA-256")');
    expect(ios).toContain("SubjectPublicKeyInfo");
    expect(ios).toContain("SHA256.hash(data: spki)");
    expect(ios).toContain("sec_protocol_options_set_verify_block");
  });
});
