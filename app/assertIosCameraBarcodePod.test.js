import { describe, expect, test } from "bun:test";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

const root = join(import.meta.dir, "..");
const script = join(root, "scripts", "assert-ios-camera-barcode-pod.sh");
/** Bounded scratch root via Node standard API — never hard-code OS global temp. */
const scratchRoot = tmpdir();

function runAssert(lockDir) {
  const lockPath = join(lockDir, "Podfile.lock");
  return spawnSync("bash", [script, lockPath], {
    encoding: "utf8",
    cwd: root,
  });
}

describe("assert-ios-camera-barcode-pod.sh", () => {
  test("passes when ExpoCameraBarcodeScanning and ZXingObjC are present", () => {
    const dir = mkdtempSync(join(scratchRoot, "zen-cam-pod-ok-"));
    try {
      writeFileSync(
        join(dir, "Podfile.lock"),
        [
          "PODS:",
          "  - ExpoCameraBarcodeScanning (1.0.0):",
          "  - ZXingObjC (3.6.9):",
          "",
        ].join("\n"),
      );
      const result = runAssert(dir);
      expect(result.status).toBe(0);
      expect(result.stdout).toContain(
        "ExpoCameraBarcodeScanning and ZXingObjC",
      );
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });

  test("fails when ZXingObjC is missing", () => {
    const dir = mkdtempSync(join(scratchRoot, "zen-cam-pod-zx-"));
    try {
      writeFileSync(
        join(dir, "Podfile.lock"),
        ["PODS:", "  - ExpoCameraBarcodeScanning (1.0.0):", ""].join("\n"),
      );
      const result = runAssert(dir);
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain("ZXingObjC");
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });

  test("fails when sibling Podfile.properties.json disables barcode scanning", () => {
    const dir = mkdtempSync(join(scratchRoot, "zen-cam-pod-off-"));
    try {
      writeFileSync(
        join(dir, "Podfile.lock"),
        [
          "PODS:",
          "  - ExpoCameraBarcodeScanning (1.0.0):",
          "  - ZXingObjC (3.6.9):",
          "",
        ].join("\n"),
      );
      writeFileSync(
        join(dir, "Podfile.properties.json"),
        JSON.stringify({ "expo.camera.barcode-scanner-enabled": "false" }),
      );
      const result = runAssert(dir);
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain(
        "expo.camera.barcode-scanner-enabled=false",
      );
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});
