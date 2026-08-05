import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir);
const iosPath = join(root, "ios/ZenLinkTransportModule.swift");

/**
 * Catch the beta.9 Release-iphoneos class of failure: a nested `private` type
 * used in a non-private method signature (Swift then requires the method to be
 * private, which breaks same-file Module consumers of the completion payload).
 */
function nestedPrivateTypesUsedOutsideEnclosingType(source: string): string[] {
  const violations: string[] = [];
  // Only indented nested types: top-level `private` is file-scoped and intentional.
  const nestedPrivate = [
    ...source.matchAll(
      /^[ \t]+private\s+(?:final\s+)?(?:struct|class|enum)\s+(\w+)\b/gm,
    ),
  ];

  for (const match of nestedPrivate) {
    const typeName = match[1];
    const signatureUses = [
      ...source.matchAll(
        new RegExp(
          String.raw`^[ \t]*(?:(?:public|open|internal|fileprivate)\s+)?(?:func|init)\b[^{\n]*\b${typeName}\b`,
          "gm",
        ),
      ),
    ].filter((use) => !/^[ \t]*private\s+(?:func|init)\b/.test(use[0]));

    if (signatureUses.length > 0) {
      violations.push(
        `${typeName} appears in non-private signature(s); use fileprivate (or lift) so same-file callers can compile`,
      );
    }
  }

  return violations;
}

describe("Zen Link native pin contract", () => {
  test("Android and iOS both pin TLS 1.3 SPKI and bind loopback only", () => {
    const android = readFileSync(
      join(
        root,
        "android/src/main/java/expo/modules/zenlinktransport/ZenLinkTransportModule.kt",
      ),
      "utf8",
    );
    const ios = readFileSync(iosPath, "utf8");

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

  test("iOS StartResult stays file-visible for Module start(completion:)", () => {
    const ios = readFileSync(iosPath, "utf8");

    // Word-boundary required: /private struct/ also matches inside "fileprivate struct".
    expect(ios).not.toMatch(/\bprivate\s+struct\s+StartResult\b/);
    expect(ios).toMatch(/\bfileprivate\s+struct\s+StartResult\b/);
    expect(ios).toMatch(
      /func start\(completion:\s*@escaping\s*\(Result<StartResult,\s*Error>\)\s*->\s*Void\)/,
    );
    expect(ios).toContain('["port": response.port, "rttMs": response.rttMilliseconds]');
    expect(nestedPrivateTypesUsedOutsideEnclosingType(ios)).toEqual([]);
  });
});
