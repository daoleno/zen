export type TurnFocusPhase = "idle" | "awaiting-row" | "arming" | "active";

export type TurnFocusCancelReason =
  "touch" | "drag" | "momentum" | "selection" | "lifecycle";

export type TurnFocusSpacerRequest = {
  height: number;
  requestEpoch: number;
};

export type TurnFocusState = {
  generation: string;
  phase: TurnFocusPhase;
  pendingMessageId?: string;
  intentToken?: number;
  reducedMotion: boolean;
  viewportHeight: number;
  topChromeInset: number;
  clearance: number;
  latestOffset: number;
  clearanceSampledForIntent: boolean;
  spacerRequestEpoch: number;
  spacerLayoutHeight?: number;
  spacerLayoutEpoch?: number;
  rowHeight?: number;
  newestEdgeOffset?: number;
  deferredRowHeight?: number;
  deferredNewestEdgeOffset?: number;
  deferredRowRequestEpoch?: number;
  spacerHeight: number;
  anchorAvailable: boolean;
  anchorRevealIssued: boolean;
};

export type TurnFocusEvent =
  | {
      type: "intent";
      generation: string;
      pendingMessageId: string;
      intentToken: number;
      reducedMotion: boolean;
    }
  | {
      type: "anchor_available";
      generation: string;
      pendingMessageId: string;
      latestOffset: number;
    }
  | {
      type: "row_layout";
      generation: string;
      pendingMessageId: string;
      height: number;
      newestEdgeOffset?: number;
    }
  | {
      type: "geometry";
      viewportHeight: number;
      topChromeInset: number;
    }
  | {
      type: "clearance_sample";
      intentToken: number;
      clearance: number;
      latestOffset: number;
    }
  | { type: "spacer_layout"; height: number; requestEpoch: number }
  | { type: "cancel"; reason: TurnFocusCancelReason }
  | { type: "reset"; generation: string };

export type TurnFocusEffect =
  | {
      type: "reveal_anchor";
      pendingMessageId: string;
      animated: false;
      latestOffset: number;
    }
  | {
      type: "return_to_latest";
      pendingMessageId: string;
      animated: boolean;
      latestOffset: number;
    };

export type TurnFocusTransition = {
  state: TurnFocusState;
  effect?: TurnFocusEffect;
};

const GEOMETRY_EPSILON = 0.5;

export function resolveTurnFocusAnchorItemId(
  pendingMessageId: string | undefined,
  items: ReadonlyArray<{ id: string; turnFocusAnchorId?: string }>,
) {
  if (!pendingMessageId) {
    return undefined;
  }
  const pendingItem = items.find((item) => item.id === pendingMessageId);
  if (pendingItem) {
    return pendingItem.id;
  }
  return items.find((item) => item.turnFocusAnchorId === pendingMessageId)?.id;
}

export function turnFocusRowGeometryFromCell(
  pendingMessageId: string | undefined,
  cellItemId: string,
  cellLayout: { height: number; y: number },
  turnFocusAnchorItemId: string | undefined = pendingMessageId,
) {
  if (!pendingMessageId || cellItemId !== turnFocusAnchorItemId) {
    return undefined;
  }
  return {
    pendingMessageId,
    height: cellLayout.height,
    newestEdgeOffset: nonNegativeFinite(cellLayout.y),
  };
}

export function turnFocusOwnsMomentum(
  phase: TurnFocusPhase,
  automaticReturnsInFlight: number,
) {
  return phase !== "idle" || automaticReturnsInFlight > 0;
}

export function turnFocusSuppressesOrdinaryFollow(state: TurnFocusState) {
  return (
    state.phase !== "idle" ||
    state.spacerLayoutEpoch !== state.spacerRequestEpoch ||
    (state.spacerLayoutHeight ?? 0) > GEOMETRY_EPSILON
  );
}

export function shouldClearTurnFocusForSurfaceLifecycle(
  surfaceActive: boolean,
  subscriptionGeneration: number,
) {
  return !surfaceActive || subscriptionGeneration > 1;
}

export function createTurnFocusState(generation: string): TurnFocusState {
  return {
    generation,
    phase: "idle",
    reducedMotion: false,
    viewportHeight: 0,
    topChromeInset: 0,
    clearance: 0,
    latestOffset: 0,
    clearanceSampledForIntent: false,
    spacerRequestEpoch: 0,
    spacerHeight: 0,
    anchorAvailable: false,
    anchorRevealIssued: false,
  };
}

export function reduceTurnFocus(
  state: TurnFocusState,
  event: TurnFocusEvent,
): TurnFocusTransition {
  switch (event.type) {
    case "intent": {
      if (
        event.generation !== state.generation ||
        !event.pendingMessageId ||
        !isPositiveFinite(event.intentToken)
      ) {
        return { state };
      }
      return {
        state: requestSpacerHeight(
          {
            ...state,
            phase: "awaiting-row",
            pendingMessageId: event.pendingMessageId,
            intentToken: event.intentToken,
            reducedMotion: event.reducedMotion,
            clearanceSampledForIntent: false,
            anchorAvailable: false,
            anchorRevealIssued: false,
            rowHeight: undefined,
            newestEdgeOffset: undefined,
            deferredRowHeight: undefined,
            deferredNewestEdgeOffset: undefined,
            deferredRowRequestEpoch: undefined,
          },
          0,
          true,
        ),
      };
    }
    case "anchor_available": {
      if (
        event.generation !== state.generation ||
        event.pendingMessageId !== state.pendingMessageId ||
        !Number.isFinite(event.latestOffset) ||
        state.phase !== "awaiting-row" ||
        state.anchorRevealIssued
      ) {
        return { state };
      }
      return revealAndArmAnchorIfReady({
        ...state,
        anchorAvailable: true,
        latestOffset: event.latestOffset,
      });
    }
    case "row_layout": {
      if (
        event.generation !== state.generation ||
        event.pendingMessageId !== state.pendingMessageId ||
        !isPositiveFinite(event.height)
      ) {
        return { state };
      }
      if (state.phase === "awaiting-row") {
        if (
          state.spacerLayoutHeight == null ||
          state.spacerLayoutHeight > GEOMETRY_EPSILON
        ) {
          return { state };
        }
        const positioned = {
          ...state,
          rowHeight: event.height,
          newestEdgeOffset: baseNewestEdgeOffset(state, event),
        };
        if (!isSpacerLayoutZero(state)) {
          // A fresh zero request can leave an already-mounted cell positioned
          // before its correlated UI-thread measurement reaches JS. Preserve
          // that candidate only while the last physical spacer truth is zero;
          // any observed nonzero height below invalidates it.
          return { state: positioned };
        }
        return commitTurnFocusFromRow(armTurnFocusIfReady(positioned));
      }
      if (state.phase !== "arming" && state.phase !== "active") {
        return { state };
      }
      if (
        state.spacerLayoutHeight == null ||
        state.spacerLayoutEpoch !== state.spacerRequestEpoch ||
        !geometryEqual(state.spacerHeight, state.spacerLayoutHeight)
      ) {
        if (positionedRowConfirmsSpacerRequest(state, event)) {
          // The affected target cell is native layout proof too. Fabric can
          // reposition it for the current spacer request without emitting the
          // spacer header's own onLayout callback. Consume that proof only
          // after the positioned row reports the requested displacement.
          return commitTurnFocusFromRow(
            updateActiveGeometry({
              ...state,
              spacerLayoutHeight: state.spacerHeight,
              spacerLayoutEpoch: state.spacerRequestEpoch,
              rowHeight: event.height,
              newestEdgeOffset: Math.max(
                0,
                nonNegativeFinite(event.newestEdgeOffset ?? 0) -
                  state.spacerHeight,
              ),
              deferredRowHeight: undefined,
              deferredNewestEdgeOffset: undefined,
              deferredRowRequestEpoch: undefined,
            }),
          );
        }
        return {
          state: deferPositionedRow(state, event),
        };
      }
      return commitTurnFocusFromRow(
        updateActiveGeometry({
          ...state,
          rowHeight: event.height,
          newestEdgeOffset: baseNewestEdgeOffset(state, event),
          deferredRowHeight: undefined,
          deferredNewestEdgeOffset: undefined,
          deferredRowRequestEpoch: undefined,
        }),
      );
    }
    case "geometry": {
      const next = {
        ...state,
        viewportHeight: nonNegativeFinite(event.viewportHeight),
        topChromeInset: nonNegativeFinite(event.topChromeInset),
      };
      if (next.phase === "awaiting-row") {
        return armTurnFocusIfReady(next);
      }
      if (next.phase !== "arming" && next.phase !== "active") {
        return { state: next };
      }
      return updateActiveGeometry(next);
    }
    case "spacer_layout": {
      if (
        !Number.isFinite(event.height) ||
        !Number.isSafeInteger(event.requestEpoch) ||
        event.requestEpoch < 0 ||
        event.requestEpoch > state.spacerRequestEpoch ||
        (state.spacerLayoutEpoch != null &&
          event.requestEpoch < state.spacerLayoutEpoch)
      ) {
        return { state };
      }
      const spacerLayoutHeight = nonNegativeFinite(event.height);
      // The mounted header reports native physical truth together with the
      // SharedValue request epoch observed by the same UI-thread measurement.
      // A nonzero physical spacer invalidates any row stored while awaiting
      // zero; an obsolete zero can retain that candidate but cannot arm it.
      const observed = {
        ...state,
        spacerLayoutHeight,
        spacerLayoutEpoch: event.requestEpoch,
        rowHeight:
          state.phase === "awaiting-row" &&
          spacerLayoutHeight > GEOMETRY_EPSILON
            ? undefined
            : state.rowHeight,
        newestEdgeOffset:
          state.phase === "awaiting-row" &&
          spacerLayoutHeight > GEOMETRY_EPSILON
            ? undefined
            : state.newestEdgeOffset,
      };
      if (observed.phase === "awaiting-row") {
        return revealAndArmAnchorIfReady(observed);
      }
      // Fabric may deliver the affected positioned row before this header.
      // A row held under the same request epoch becomes consumable only now,
      // after the header reports matching native physical truth.
      return commitDeferredPositionedRow(observed);
    }
    case "clearance_sample": {
      if (
        state.phase === "idle" ||
        event.intentToken !== state.intentToken ||
        !Number.isFinite(event.clearance) ||
        !Number.isFinite(event.latestOffset)
      ) {
        return { state };
      }
      const next = {
        ...state,
        clearance: nonNegativeFinite(event.clearance),
        latestOffset: event.latestOffset,
        clearanceSampledForIntent: true,
      };
      if (next.phase === "awaiting-row") {
        return armTurnFocusIfReady(next);
      }
      return updateActiveGeometry(next);
    }
    case "cancel": {
      if (state.phase === "idle" && state.spacerHeight === 0) {
        return { state };
      }
      return { state: clearTurnFocus(state) };
    }
    case "reset": {
      if (
        event.generation === state.generation &&
        state.phase === "idle" &&
        state.spacerHeight === 0
      ) {
        return { state };
      }
      return {
        state: {
          ...clearTurnFocus(state),
          generation: event.generation,
        },
      };
    }
  }
}

function armTurnFocusIfReady(state: TurnFocusState): TurnFocusTransition {
  if (
    state.phase !== "awaiting-row" ||
    !state.pendingMessageId ||
    !isPositiveFinite(state.rowHeight) ||
    state.viewportHeight <= 0 ||
    !state.clearanceSampledForIntent ||
    !isSpacerLayoutZero(state)
  ) {
    return { state };
  }
  const armed = requestSpacerHeight(
    {
      ...state,
      phase: "arming",
    },
    turnFocusSpacerHeight(state, state.rowHeight),
  );
  if (armed.spacerHeight <= GEOMETRY_EPSILON) {
    // No native spacer transaction is required: the stored exact row was
    // observed while the mounted header was already zero. This also avoids a
    // probe when the correlated clearance sample arrives after that row.
    return finishReturn(clearTurnFocus(armed), state);
  }
  return { state: armed };
}

function commitTurnFocusFromRow(
  transition: TurnFocusTransition,
): TurnFocusTransition {
  const state = transition.state;
  if (
    state.spacerLayoutHeight == null ||
    state.spacerLayoutEpoch !== state.spacerRequestEpoch ||
    !geometryEqual(state.spacerHeight, state.spacerLayoutHeight)
  ) {
    return transition;
  }
  if (state.phase === "arming" && state.pendingMessageId) {
    if (state.spacerHeight <= GEOMETRY_EPSILON) {
      return finishReturn(clearTurnFocus(state), state);
    }
    return finishReturn({ ...state, phase: "active" }, state);
  }
  if (state.phase === "active" && state.spacerHeight <= GEOMETRY_EPSILON) {
    return { state: clearTurnFocus(state) };
  }
  return transition;
}

function updateActiveGeometry(state: TurnFocusState): TurnFocusTransition {
  if (!isPositiveFinite(state.rowHeight)) {
    return { state };
  }
  const spacerHeight = turnFocusSpacerHeight(state, state.rowHeight);
  return {
    state: requestSpacerHeight(state, spacerHeight),
  };
}

function finishReturn(
  next: TurnFocusState,
  previous: TurnFocusState,
): TurnFocusTransition {
  if (!previous.pendingMessageId) {
    return { state: next };
  }
  return {
    state: next,
    effect: {
      type: "return_to_latest",
      pendingMessageId: previous.pendingMessageId,
      animated: !previous.reducedMotion,
      latestOffset: previous.latestOffset,
    },
  };
}

function clearTurnFocus(state: TurnFocusState): TurnFocusState {
  return requestSpacerHeight(
    {
      ...state,
      phase: "idle",
      pendingMessageId: undefined,
      intentToken: undefined,
      reducedMotion: false,
      clearanceSampledForIntent: false,
      anchorAvailable: false,
      anchorRevealIssued: false,
      rowHeight: undefined,
      newestEdgeOffset: undefined,
      deferredRowHeight: undefined,
      deferredNewestEdgeOffset: undefined,
      deferredRowRequestEpoch: undefined,
    },
    0,
  );
}

function turnFocusSpacerHeight(state: TurnFocusState, rowHeight: number) {
  const usableViewportHeight = Math.max(
    0,
    state.viewportHeight - state.topChromeInset - state.clearance,
  );
  return clamp(
    usableViewportHeight - rowHeight - (state.newestEdgeOffset ?? 0),
    0,
    usableViewportHeight,
  );
}

function revealAnchorIfReady(state: TurnFocusState): TurnFocusTransition {
  if (
    state.phase !== "awaiting-row" ||
    !state.pendingMessageId ||
    !state.anchorAvailable ||
    state.anchorRevealIssued ||
    !isSpacerLayoutZero(state)
  ) {
    return { state };
  }
  return {
    state: {
      ...state,
      anchorRevealIssued: true,
    },
    effect: {
      type: "reveal_anchor",
      pendingMessageId: state.pendingMessageId,
      animated: false,
      latestOffset: state.latestOffset,
    },
  };
}

function revealAndArmAnchorIfReady(state: TurnFocusState): TurnFocusTransition {
  const reveal = revealAnchorIfReady(state);
  const armed = armTurnFocusIfReady(reveal.state);
  return {
    state: armed.state,
    effect: armed.effect ?? reveal.effect,
  };
}

function baseNewestEdgeOffset(
  state: TurnFocusState,
  event: Extract<TurnFocusEvent, { type: "row_layout" }>,
) {
  return Math.max(
    0,
    nonNegativeFinite(event.newestEdgeOffset ?? 0) -
      (state.spacerLayoutHeight ?? 0),
  );
}

function deferPositionedRow(
  state: TurnFocusState,
  event: Extract<TurnFocusEvent, { type: "row_layout" }>,
): TurnFocusState {
  return {
    ...state,
    deferredRowHeight: event.height,
    deferredNewestEdgeOffset: nonNegativeFinite(event.newestEdgeOffset ?? 0),
    deferredRowRequestEpoch: state.spacerRequestEpoch,
  };
}

function commitDeferredPositionedRow(
  state: TurnFocusState,
): TurnFocusTransition {
  if (
    (state.phase !== "arming" && state.phase !== "active") ||
    !isPositiveFinite(state.deferredRowHeight) ||
    state.deferredNewestEdgeOffset == null ||
    state.deferredRowRequestEpoch !== state.spacerRequestEpoch ||
    state.spacerLayoutEpoch !== state.spacerRequestEpoch ||
    state.spacerLayoutHeight == null ||
    !geometryEqual(state.spacerHeight, state.spacerLayoutHeight)
  ) {
    return { state };
  }
  return commitTurnFocusFromRow(
    updateActiveGeometry({
      ...state,
      rowHeight: state.deferredRowHeight,
      newestEdgeOffset: Math.max(
        0,
        state.deferredNewestEdgeOffset - state.spacerLayoutHeight,
      ),
      deferredRowHeight: undefined,
      deferredNewestEdgeOffset: undefined,
      deferredRowRequestEpoch: undefined,
    }),
  );
}

function positionedRowConfirmsSpacerRequest(
  state: TurnFocusState,
  event: Extract<TurnFocusEvent, { type: "row_layout" }>,
) {
  if (
    state.spacerHeight <= GEOMETRY_EPSILON ||
    state.newestEdgeOffset == null ||
    event.newestEdgeOffset == null
  ) {
    return false;
  }
  const positionedOffset = nonNegativeFinite(event.newestEdgeOffset);
  const expectedOffset = state.spacerHeight + state.newestEdgeOffset;
  if (state.phase === "arming") {
    // At initial arm the preceding correlated zero transaction rules out an
    // old spacer. Additional Activity/streaming growth may land in the same
    // Fabric layout pass, so an offset beyond the request is still causal.
    return positionedOffset + GEOMETRY_EPSILON >= expectedOffset;
  }
  return geometryEqual(positionedOffset, expectedOffset);
}

function isSpacerLayoutZero(state: TurnFocusState) {
  // SharedValue writes reach native layout asynchronously. Only the mounted
  // header's observation of the current request epoch proves an older spacer
  // is gone.
  return (
    state.spacerLayoutEpoch === state.spacerRequestEpoch &&
    state.spacerLayoutHeight != null &&
    state.spacerLayoutHeight <= GEOMETRY_EPSILON
  );
}

function requestSpacerHeight(
  state: TurnFocusState,
  requestedHeight: number,
  forceTransaction: boolean = false,
): TurnFocusState {
  const spacerHeight = nonNegativeFinite(requestedHeight);
  if (geometryEqual(state.spacerHeight, spacerHeight) && !forceTransaction) {
    return state;
  }
  return {
    ...state,
    spacerHeight,
    spacerRequestEpoch: state.spacerRequestEpoch + 1,
    deferredRowHeight: undefined,
    deferredNewestEdgeOffset: undefined,
    deferredRowRequestEpoch: undefined,
  };
}

function nonNegativeFinite(value: number) {
  return Number.isFinite(value) ? Math.max(0, value) : 0;
}

function isPositiveFinite(value: number | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value) && value > 0;
}

function clamp(value: number, minimum: number, maximum: number) {
  return Math.min(maximum, Math.max(minimum, value));
}

function geometryEqual(left: number, right: number) {
  return Math.abs(left - right) <= GEOMETRY_EPSILON;
}
