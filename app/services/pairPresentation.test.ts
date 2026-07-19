import { describe, expect, test } from "bun:test";
import {
  closePairPresentation,
  completePairImport,
  createClosedPairPresentation,
  lockPairScanner,
  openPairEditor,
  openPairScanner,
  resolvePairPresentationDismiss,
  returnToPairEditor,
  unlockPairScanner,
} from "./pairPresentation";

describe("Pair presentation single Modal owner", () => {
  test("opens editor then scanner without a second presentation owner", () => {
    const editor = openPairEditor(createClosedPairPresentation());
    expect(editor).toEqual({ mode: "editor", scannerLocked: false });
    expect(resolvePairPresentationDismiss(editor.mode)).toBe("close");

    const scanner = openPairScanner(editor);
    expect(scanner).toEqual({ mode: "scanner", scannerLocked: false });
    expect(resolvePairPresentationDismiss(scanner.mode)).toBe(
      "return-to-editor",
    );
  });

  test("scanner back restores editor; import success closes the presentation", () => {
    const browsing = openPairScanner(openPairEditor());
    const locked = lockPairScanner(browsing);
    expect(locked.scannerLocked).toBe(true);

    expect(returnToPairEditor(locked)).toEqual({
      mode: "editor",
      scannerLocked: false,
    });

    expect(unlockPairScanner(lockPairScanner(browsing)).scannerLocked).toBe(
      false,
    );
    expect(completePairImport()).toEqual(createClosedPairPresentation());
    expect(closePairPresentation()).toEqual({
      mode: "closed",
      scannerLocked: false,
    });
  });
});
