/**
 * Quiet Mode mokugyo mallet — handheld wrist strike (not a fixed-grip pendulum).
 *
 * The whole SVG translates and rotates together: padded head falls from above
 * onto the upper sounding area while the grip sinks/forwards with the stroke.
 * React Native applies rotate about the view center; we only add translate + rotate.
 */

export const MOKUGYO_MALLET = {
  width: 170,
  height: 72,
  /** Cloth head center in mallet-local coords. */
  headX: 31,
  headY: 36,
  /** Handle rear / grip center in mallet-local coords. */
  gripX: 156,
  gripY: 35,
} as const;

/** Stage and instrument layout used to place the mallet wrap. */
export const MOKUGYO_STAGE = {
  width: 300,
  height: 280,
  instrumentWidth: 282,
  instrumentHeight: 218,
  /** Upper wooden-fish body near sounding slot (instrument-local). */
  contactFishX: 148,
  contactFishY: 68,
} as const;

/**
 * Fixed wrap origin in stage space. Animated translate is relative to this.
 * Chosen so rest sits clearly above the fish and contact lands on the top/back.
 */
export const MOKUGYO_WRAP = {
  left: 122,
  top: 48,
} as const;

export type MalletPoint = { x: number; y: number };

export type MalletPose = {
  translateX: number;
  translateY: number;
  rotateDeg: number;
};

/**
 * Named keyframes for the wrist strike.
 * Progress 0→1 maps rest → downstroke → contact (dwell) → impact compression.
 * Rebound is the reverse path while springing back toward rest.
 */
export const MOKUGYO_STRIKE_KEYFRAMES = {
  rest: { translateX: 16, translateY: -44, rotateDeg: -32 },
  downstroke: { translateX: 4, translateY: -4, rotateDeg: -14 },
  contact: { translateX: -8, translateY: 24, rotateDeg: 4 },
  impact: { translateX: -10, translateY: 30, rotateDeg: 8 },
  /** Documentary mid-rebound sample (progress ≈ 0.28 on the way home). */
  rebound: { translateX: 6, translateY: -18, rotateDeg: -20 },
} as const satisfies Record<string, MalletPose>;

/** Progress stops for rest → downstroke → contact dwell → impact. */
export const MOKUGYO_STRIKE_PROGRESS = {
  rest: 0,
  downstroke: 0.45,
  contact: 0.78,
  contactHold: 0.88,
  impact: 1,
  reboundSample: 0.28,
} as const;

export type MalletKeyframeName =
  | "rest"
  | "downstroke"
  | "contact"
  | "impact"
  | "rebound";

/** Clockwise-positive rotation matching React Native transform rotate. */
export function rotateClockwise(
  dx: number,
  dy: number,
  angleDeg: number,
): MalletPoint {
  const radians = (angleDeg * Math.PI) / 180;
  const cos = Math.cos(radians);
  const sin = Math.sin(radians);
  return {
    x: dx * cos + dy * sin,
    y: -dx * sin + dy * cos,
  };
}

export function instrumentOriginInStage(): MalletPoint {
  return {
    x: (MOKUGYO_STAGE.width - MOKUGYO_STAGE.instrumentWidth) / 2,
    y: (MOKUGYO_STAGE.height - MOKUGYO_STAGE.instrumentHeight) / 2,
  };
}

export function contactPointInStage(): MalletPoint {
  const origin = instrumentOriginInStage();
  return {
    x: origin.x + MOKUGYO_STAGE.contactFishX,
    y: origin.y + MOKUGYO_STAGE.contactFishY,
  };
}

export function malletWrapPlacement(): { left: number; top: number } {
  return { left: MOKUGYO_WRAP.left, top: MOKUGYO_WRAP.top };
}

/**
 * Map strike progress 0 (rest) → 1 (impact) onto a handheld pose.
 * Contact is held briefly before the compression tip.
 */
export function malletStrikePose(progress: number): MalletPose {
  const t = clamp01(progress);
  const { rest, downstroke, contact, impact } = MOKUGYO_STRIKE_KEYFRAMES;
  const p = MOKUGYO_STRIKE_PROGRESS;

  if (t <= p.downstroke) {
    return lerpPose(rest, downstroke, t / p.downstroke);
  }
  if (t <= p.contact) {
    return lerpPose(
      downstroke,
      contact,
      (t - p.downstroke) / (p.contact - p.downstroke),
    );
  }
  if (t <= p.contactHold) {
    return { ...contact };
  }
  return lerpPose(
    contact,
    impact,
    (t - p.contactHold) / (p.impact - p.contactHold),
  );
}

/** Named keyframe pose (rebound uses the documentary sample pose). */
export function malletKeyframePose(name: MalletKeyframeName): MalletPose {
  return { ...MOKUGYO_STRIKE_KEYFRAMES[name] };
}

/**
 * Stage-space position of a mallet-local point after translate + rotate-about-center.
 * Matches RN: transform [{translateX},{translateY},{rotate}] (rotate about view center).
 */
export function malletLocalPointInStage(
  local: MalletPoint,
  pose: MalletPose,
  wrap: { left: number; top: number } = MOKUGYO_WRAP,
): MalletPoint {
  const centerX = MOKUGYO_MALLET.width / 2;
  const centerY = MOKUGYO_MALLET.height / 2;
  const rotated = rotateClockwise(
    local.x - centerX,
    local.y - centerY,
    pose.rotateDeg,
  );
  return {
    x: wrap.left + centerX + rotated.x + pose.translateX,
    y: wrap.top + centerY + rotated.y + pose.translateY,
  };
}

export function malletHeadInStage(pose: MalletPose): MalletPoint {
  return malletLocalPointInStage(
    { x: MOKUGYO_MALLET.headX, y: MOKUGYO_MALLET.headY },
    pose,
  );
}

export function malletGripInStage(pose: MalletPose): MalletPoint {
  return malletLocalPointInStage(
    { x: MOKUGYO_MALLET.gripX, y: MOKUGYO_MALLET.gripY },
    pose,
  );
}

/** Four acceptance keyframes with stage coordinates for visual QA / tests. */
export function malletStrikeKeyframeSheet(): Array<{
  name: MalletKeyframeName;
  progress: number;
  pose: MalletPose;
  head: MalletPoint;
  grip: MalletPoint;
}> {
  return (
    [
      ["rest", MOKUGYO_STRIKE_PROGRESS.rest],
      ["downstroke", MOKUGYO_STRIKE_PROGRESS.downstroke],
      ["contact", MOKUGYO_STRIKE_PROGRESS.contact],
      ["rebound", MOKUGYO_STRIKE_PROGRESS.reboundSample],
    ] as const
  ).map(([name, progress]) => {
    const pose =
      name === "rebound"
        ? malletKeyframePose("rebound")
        : malletStrikePose(progress);
    return {
      name,
      progress,
      pose,
      head: malletHeadInStage(pose),
      grip: malletGripInStage(pose),
    };
  });
}

/**
 * Reanimated-safe transform: whole-mallet translate + rotate about view center.
 * Grip is not pinned — it travels with translateY/X.
 */
export function malletStrikeTransforms(progress: number): Array<
  { translateX: number } | { translateY: number } | { rotate: string }
> {
  const pose = malletStrikePose(progress);
  return [
    { translateX: pose.translateX },
    { translateY: pose.translateY },
    { rotate: `${pose.rotateDeg}deg` },
  ];
}

/** Sample head/grip trajectory for monotonicity and rapid-tap continuity checks. */
export function sampleStrikeTrajectory(steps = 12): Array<{
  progress: number;
  pose: MalletPose;
  head: MalletPoint;
  grip: MalletPoint;
}> {
  const samples: Array<{
    progress: number;
    pose: MalletPose;
    head: MalletPoint;
    grip: MalletPoint;
  }> = [];
  for (let i = 0; i <= steps; i += 1) {
    const progress = i / steps;
    const pose = malletStrikePose(progress);
    samples.push({
      progress,
      pose,
      head: malletHeadInStage(pose),
      grip: malletGripInStage(pose),
    });
  }
  return samples;
}

/**
 * Rapid-tap restart: blending from an interrupted progress toward impact stays
 * continuous (no teleport larger than the natural stroke delta at that step).
 */
export function rapidTapPoseDelta(
  fromProgress: number,
  toProgress: number,
): { head: number; grip: number } {
  const from = malletStrikePose(fromProgress);
  const to = malletStrikePose(toProgress);
  const fromHead = malletHeadInStage(from);
  const toHead = malletHeadInStage(to);
  const fromGrip = malletGripInStage(from);
  const toGrip = malletGripInStage(to);
  return {
    head: Math.hypot(toHead.x - fromHead.x, toHead.y - fromHead.y),
    grip: Math.hypot(toGrip.x - fromGrip.x, toGrip.y - fromGrip.y),
  };
}

function clamp01(value: number): number {
  if (value <= 0) {
    return 0;
  }
  if (value >= 1) {
    return 1;
  }
  return value;
}

function lerp(from: number, to: number, t: number): number {
  return from + (to - from) * t;
}

function lerpPose(from: MalletPose, to: MalletPose, t: number): MalletPose {
  const u = clamp01(t);
  return {
    translateX: lerp(from.translateX, to.translateX, u),
    translateY: lerp(from.translateY, to.translateY, u),
    rotateDeg: lerp(from.rotateDeg, to.rotateDeg, u),
  };
}
