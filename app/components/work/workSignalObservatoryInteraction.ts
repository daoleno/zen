export const WORK_OBSERVATORY_PULL = {
  threshold: 132,
  activationDistance: 8,
  drawerEdgeExclusion: 24,
  horizontalFailDistance: 12,
  axisDominance: 1.25,
} as const;

export const WORK_OBSERVATORY_ACCESSIBILITY_ACTION = {
  name: "open-work-observatory",
  label: "Open Work",
} as const;

export const WORK_OBSERVATORY_ACCESSIBILITY_ACTIONS = [
  WORK_OBSERVATORY_ACCESSIBILITY_ACTION,
] as const;

export type WorkObservatoryPullIntent = "pending" | "activate" | "fail";

export function dispatchWorkObservatoryAccessibilityAction(
  actionName: string,
  open: () => void,
): boolean {
  if (actionName !== WORK_OBSERVATORY_ACCESSIBILITY_ACTION.name) {
    return false;
  }
  open();
  return true;
}

export function createWorkObservatoryAccessibilityProps(open: () => void): {
  accessibilityActions: typeof WORK_OBSERVATORY_ACCESSIBILITY_ACTIONS;
  onAccessibilityAction(event: {
    nativeEvent: { actionName: string };
  }): void;
} {
  return {
    accessibilityActions: WORK_OBSERVATORY_ACCESSIBILITY_ACTIONS,
    onAccessibilityAction: (event) => {
      dispatchWorkObservatoryAccessibilityAction(
        event.nativeEvent.actionName,
        open,
      );
    },
  };
}

export function resolveWorkObservatoryMotion(reducedMotion: boolean): {
  modalAnimationType: "none" | "fade";
} {
  return {
    modalAnimationType: reducedMotion ? "none" : "fade",
  };
}

export function resolveWorkObservatoryPullIntent({
  touchCount,
  startX,
  dx,
  dy,
  scrollOffsetY,
}: {
  touchCount: number;
  startX: number;
  dx: number;
  dy: number;
  scrollOffsetY: number;
}): WorkObservatoryPullIntent {
  "worklet";
  if (
    touchCount !== 1 ||
    startX <= WORK_OBSERVATORY_PULL.drawerEdgeExclusion ||
    scrollOffsetY > 0 ||
    dy <= -WORK_OBSERVATORY_PULL.activationDistance
  ) {
    return "fail";
  }
  const absoluteDx = Math.abs(dx);
  const absoluteDy = Math.abs(dy);
  if (
    absoluteDx >= WORK_OBSERVATORY_PULL.horizontalFailDistance &&
    absoluteDx >= WORK_OBSERVATORY_PULL.axisDominance * absoluteDy
  ) {
    return "fail";
  }
  return dy > WORK_OBSERVATORY_PULL.activationDistance &&
    absoluteDy > WORK_OBSERVATORY_PULL.axisDominance * absoluteDx
    ? "activate"
    : "pending";
}

export function shouldRevealWorkObservatory(distance: number): boolean {
  "worklet";
  return (
    Number.isFinite(distance) && distance >= WORK_OBSERVATORY_PULL.threshold
  );
}
