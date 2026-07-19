import { describe, expect, test } from "bun:test";
import {
  attemptDismissPairScanner,
  createPairScanClaim,
  isPairScanClaimHeld,
  releasePairScan,
  shouldDismissPairScanner,
  tryClaimPairScan,
} from "./pairScanClaim";

describe("Pair scan claim gate", () => {
  test("only the first concurrent claim enters the import path", () => {
    const claim = createPairScanClaim();
    expect(tryClaimPairScan(claim)).toBe(true);
    expect(isPairScanClaimHeld(claim)).toBe(true);
    expect(tryClaimPairScan(claim)).toBe(false);
    expect(tryClaimPairScan(claim)).toBe(false);
  });

  test("picker claim blocks live camera until release", () => {
    const claim = createPairScanClaim();
    expect(tryClaimPairScan(claim)).toBe(true);
    expect(tryClaimPairScan(claim)).toBe(false);
  });

  test("scanner dismissal while claim held does not release the latch", () => {
    const claim = createPairScanClaim();
    expect(tryClaimPairScan(claim)).toBe(true);
    expect(shouldDismissPairScanner(claim)).toBe(false);
    expect(attemptDismissPairScanner(claim)).toBe("blocked");
    // Critical: Done/backdrop/system-back must leave the claim held for import finally.
    expect(isPairScanClaimHeld(claim)).toBe(true);
    releasePairScan(claim);
    expect(attemptDismissPairScanner(claim)).toBe("dismiss");
    expect(isPairScanClaimHeld(claim)).toBe(false);
  });

  test("open, close, success, and failure all release for retry when idle", () => {
    const claim = createPairScanClaim();
    expect(tryClaimPairScan(claim)).toBe(true);
    releasePairScan(claim);
    expect(isPairScanClaimHeld(claim)).toBe(false);
    expect(tryClaimPairScan(claim)).toBe(true);
    releasePairScan(claim);
    expect(tryClaimPairScan(claim)).toBe(true);
    releasePairScan(claim);
    expect(isPairScanClaimHeld(claim)).toBe(false);
  });

  test("picker cancel, no-QR, and read error release without sticky lock", () => {
    const claim = createPairScanClaim();
    for (const _path of ["cancel", "no-qr", "read-error"] as const) {
      expect(tryClaimPairScan(claim)).toBe(true);
      releasePairScan(claim);
      expect(isPairScanClaimHeld(claim)).toBe(false);
    }
  });
});
