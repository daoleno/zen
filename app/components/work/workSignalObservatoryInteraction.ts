export const WORK_OBSERVATORY_PULL = {
  threshold: 132,
  activationDistance: 8,
  drawerEdgeExclusion: 24,
  horizontalFailDistance: 12,
  axisDominance: 1.25,
} as const;

export type WorkObservatoryPullIntent = "pending" | "activate" | "fail";

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
