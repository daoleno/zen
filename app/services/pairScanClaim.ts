/**
 * Synchronous single-owner claim for Pair QR import.
 * Camera and image-picker paths share one latch so concurrent callbacks cannot
 * both enter importConnection.
 */
export type PairScanClaim = {
  held: boolean;
};

export function createPairScanClaim(): PairScanClaim {
  return { held: false };
}

/** Returns true only for the first claimant until release. */
export function tryClaimPairScan(claim: PairScanClaim): boolean {
  if (claim.held) {
    return false;
  }
  claim.held = true;
  return true;
}

export function releasePairScan(claim: PairScanClaim): void {
  claim.held = false;
}

export function isPairScanClaimHeld(claim: PairScanClaim): boolean {
  return claim.held;
}

/**
 * Scanner Done / backdrop / system-back must not dismiss while an import
 * claim is held. The latch is the authority — not lagging React state.
 */
export function shouldDismissPairScanner(claim: PairScanClaim): boolean {
  return !claim.held;
}

/**
 * Attempt scanner dismissal. When blocked, the claim stays held — dismissal
 * must never release an in-flight import latch.
 */
export function attemptDismissPairScanner(
  claim: PairScanClaim,
): "dismiss" | "blocked" {
  if (!shouldDismissPairScanner(claim)) {
    return "blocked";
  }
  return "dismiss";
}
