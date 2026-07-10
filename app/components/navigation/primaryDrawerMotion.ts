import { ReduceMotion, type WithSpringConfig } from "react-native-reanimated";

export const PrimaryDrawerMotion = {
  edgeWidth: 24,
  activationDistance: 12,
  wrongDirectionDistance: 6,
  verticalFailDistance: 10,
  horizontalDominance: 1.25,
  velocityProjectionSeconds: 0.18,
  maxVelocity: 2000,
  overlayMaxOpacity: 1,
} as const;

export const PrimaryDrawerSpring = {
  stiffness: 380,
  damping: 36,
  mass: 0.9,
  overshootClamping: true,
  reduceMotion: ReduceMotion.System,
} satisfies WithSpringConfig;

export function clampDrawerOffset(offset: number, width: number): number {
  "worklet";
  return Math.max(-width, Math.min(0, offset));
}

export function clampDrawerVelocity(velocity: number): number {
  "worklet";
  return Math.max(
    -PrimaryDrawerMotion.maxVelocity,
    Math.min(PrimaryDrawerMotion.maxVelocity, velocity),
  );
}

export function getDrawerProgress(offset: number, width: number): number {
  "worklet";
  if (width <= 0) {
    return 0;
  }
  return Math.max(0, Math.min(1, (offset + width) / width));
}

export function getProjectedDrawerTarget(
  offset: number,
  width: number,
  velocity: number,
): "closed" | "open" {
  "worklet";
  const progress = getDrawerProgress(offset, width);
  const projectedProgress = Math.max(
    0,
    Math.min(
      1,
      progress +
        (clampDrawerVelocity(velocity) *
          PrimaryDrawerMotion.velocityProjectionSeconds) /
          width,
    ),
  );
  return projectedProgress >= 0.5 ? "open" : "closed";
}
