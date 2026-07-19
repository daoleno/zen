/**
 * Pair Server presentation owns exactly one Modal. Editor and scanner are
 * modes of that owner — never sibling Modals.
 */
export type PairPresentationMode = "closed" | "editor" | "scanner";

export type PairPresentationState = {
  mode: PairPresentationMode;
  scannerLocked: boolean;
};

export function createClosedPairPresentation(): PairPresentationState {
  return {
    mode: "closed",
    scannerLocked: false,
  };
}

export function openPairEditor(
  _state: PairPresentationState = createClosedPairPresentation(),
): PairPresentationState {
  return {
    mode: "editor",
    scannerLocked: false,
  };
}

/** Enter in-card/fullscreen scanner without dismissing the pair Modal. */
export function openPairScanner(
  state: PairPresentationState,
): PairPresentationState {
  if (state.mode === "closed") {
    return state;
  }
  return {
    mode: "scanner",
    scannerLocked: false,
  };
}

/** Leave scanner and restore the editor; drafts stay with the screen state. */
export function returnToPairEditor(
  state: PairPresentationState,
): PairPresentationState {
  if (state.mode !== "scanner") {
    return state;
  }
  return {
    mode: "editor",
    scannerLocked: false,
  };
}

export function closePairPresentation(): PairPresentationState {
  return createClosedPairPresentation();
}

export function lockPairScanner(
  state: PairPresentationState,
): PairPresentationState {
  if (state.mode !== "scanner") {
    return state;
  }
  return {
    ...state,
    scannerLocked: true,
  };
}

export function unlockPairScanner(
  state: PairPresentationState,
): PairPresentationState {
  return {
    ...state,
    scannerLocked: false,
  };
}

/**
 * Backdrop / system back while the pair Modal is open.
 * Scanner returns to editor; editor closes the presentation.
 */
export function resolvePairPresentationDismiss(
  mode: PairPresentationMode,
): "return-to-editor" | "close" {
  return mode === "scanner" ? "return-to-editor" : "close";
}

/** Successful paste/image/camera import releases the single Modal owner. */
export function completePairImport(): PairPresentationState {
  return createClosedPairPresentation();
}
