// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  MOKUGYO_STRIKE_KEYFRAMES,
  MOKUGYO_STRIKE_PROGRESS,
  contactPointInStage,
  malletGripInStage,
  malletHeadInStage,
  malletKeyframePose,
  malletStrikeKeyframeSheet,
  malletStrikePose,
  malletStrikeTransforms,
  malletWrapPlacement,
  rapidTapPoseDelta,
  sampleStrikeTrajectory,
} from "./mokugyoMalletGeometry";

describe("mokugyoMalletGeometry handheld wrist strike", () => {
  test("keyframe sheet: contact head Y is greater than rest (screen Y down)", () => {
    const sheet = malletStrikeKeyframeSheet();
    const rest = sheet.find((frame) => frame.name === "rest");
    const contact = sheet.find((frame) => frame.name === "contact");
    expect(rest).toBeTruthy();
    expect(contact).toBeTruthy();
    expect(contact.head.y).toBeGreaterThan(rest.head.y);
    // Head starts clearly above the fish contact band.
    expect(rest.head.y).toBeLessThan(contactPointInStage().y - 50);
    expect(contact.head.y).toBeGreaterThan(70);
    expect(contact.head.y).toBeLessThan(130);
  });

  test("grip center has non-zero stage displacement rest → contact", () => {
    const rest = malletHeadInStage(malletKeyframePose("rest"));
    const restGrip = malletGripInStage(malletKeyframePose("rest"));
    const contactGrip = malletGripInStage(malletKeyframePose("contact"));
    const dx = contactGrip.x - restGrip.x;
    const dy = contactGrip.y - restGrip.y;
    const mag = Math.hypot(dx, dy);
    expect(mag).toBeGreaterThan(20);
    // Grip sinks (Y increases) and eases forward/left toward the fish.
    expect(dy).toBeGreaterThan(15);
    expect(dx).toBeLessThan(0);
    // Not a fixed nail: grip moves more than a pixel of noise.
    expect(Math.abs(dx) + Math.abs(dy)).toBeGreaterThan(20);
    void rest;
  });

  test("downstroke head descends from rest toward contact (no bottom-up lift)", () => {
    const sheet = malletStrikeKeyframeSheet();
    const rest = sheet.find((f) => f.name === "rest");
    const down = sheet.find((f) => f.name === "downstroke");
    const contact = sheet.find((f) => f.name === "contact");
    expect(down.head.y).toBeGreaterThan(rest.head.y);
    expect(contact.head.y).toBeGreaterThan(down.head.y);
    // Downstroke is between rest and contact vertically.
    expect(down.head.y).toBeGreaterThan(rest.head.y + 20);
    expect(down.head.y).toBeLessThan(contact.head.y - 10);
  });

  test("rebound head sits between contact and rest (reverse arc settle)", () => {
    const sheet = malletStrikeKeyframeSheet();
    const rest = sheet.find((f) => f.name === "rest");
    const contact = sheet.find((f) => f.name === "contact");
    const rebound = sheet.find((f) => f.name === "rebound");
    expect(rebound.head.y).toBeGreaterThan(rest.head.y);
    expect(rebound.head.y).toBeLessThan(contact.head.y);
  });

  test("whole-mallet transforms are translate + rotate (no fixed-grip pivot compose)", () => {
    const pose = malletStrikePose(0.5);
    expect(malletStrikeTransforms(0.5)).toEqual([
      { translateX: pose.translateX },
      { translateY: pose.translateY },
      { rotate: `${pose.rotateDeg}deg` },
    ]);
    // Stroke uses non-zero translation — not rotate-only pendulum.
    expect(
      Math.abs(MOKUGYO_STRIKE_KEYFRAMES.contact.translateY -
        MOKUGYO_STRIKE_KEYFRAMES.rest.translateY),
    ).toBeGreaterThan(40);
  });

  test("head Y increases monotonically through the downswing samples", () => {
    const samples = sampleStrikeTrajectory(16);
    // Through contact hold, head should not rise.
    for (let i = 1; i < samples.length; i += 1) {
      if (samples[i].progress <= MOKUGYO_STRIKE_PROGRESS.contactHold) {
        expect(samples[i].head.y).toBeGreaterThanOrEqual(
          samples[i - 1].head.y - 0.05,
        );
      }
    }
    // Vertical travel dominates horizontal (no spear poke).
    const start = samples[0].head;
    const end = samples[samples.length - 1].head;
    expect(end.y - start.y).toBeGreaterThan(Math.abs(end.x - start.x));
  });

  test("contact dwell holds pose then impact adds light compression", () => {
    const holdA = malletStrikePose(MOKUGYO_STRIKE_PROGRESS.contact);
    const holdB = malletStrikePose(MOKUGYO_STRIKE_PROGRESS.contactHold);
    expect(holdA).toEqual(holdB);
    const impact = malletStrikePose(1);
    expect(impact.translateY).toBeGreaterThan(holdA.translateY);
    expect(impact.rotateDeg).toBeGreaterThan(holdA.rotateDeg);
  });

  test("rapid tap restart stays continuous without teleport jumps", () => {
    // Interrupted mid-stroke → retarget impact: delta must be less than full stroke.
    const full = rapidTapPoseDelta(0, 1);
    const midToImpact = rapidTapPoseDelta(0.4, 1);
    const nearToImpact = rapidTapPoseDelta(0.85, 1);
    expect(midToImpact.head).toBeLessThan(full.head);
    expect(nearToImpact.head).toBeLessThan(midToImpact.head);
    expect(nearToImpact.grip).toBeLessThan(full.grip);
    // Tiny progress steps stay small (no discontinuous snap).
    const step = rapidTapPoseDelta(0.5, 0.52);
    expect(step.head).toBeLessThan(8);
    expect(step.grip).toBeLessThan(8);
  });

  test("wrap placement keeps mallet above/diagonal of the fish at rest", () => {
    const wrap = malletWrapPlacement();
    const restHead = malletHeadInStage(malletKeyframePose("rest"));
    const contact = contactPointInStage();
    expect(wrap.left).toBeGreaterThan(100);
    expect(restHead.x).toBeGreaterThan(contact.x);
    expect(restHead.y).toBeLessThan(contact.y);
  });

  test("exports stable progress anchors for animation wiring", () => {
    expect(MOKUGYO_STRIKE_PROGRESS.rest).toBe(0);
    expect(MOKUGYO_STRIKE_PROGRESS.impact).toBe(1);
    expect(MOKUGYO_STRIKE_PROGRESS.downstroke).toBeGreaterThan(0);
    expect(MOKUGYO_STRIKE_PROGRESS.contact).toBeGreaterThan(
      MOKUGYO_STRIKE_PROGRESS.downstroke,
    );
  });
});
