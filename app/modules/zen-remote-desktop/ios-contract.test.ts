import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const moduleRoot = import.meta.dir;
const swiftSource = readFileSync(
  join(moduleRoot, "ios/ZenRemoteDesktopModule.swift"),
  "utf8",
);
const podspec = readFileSync(
  join(moduleRoot, "ZenRemoteDesktop.podspec"),
  "utf8",
);

describe("ZenRemoteDesktop iOS native contract", () => {
  test("uses only view-lifecycle APIs present in pinned ExpoModulesCore", () => {
    // Pinned ExpoModulesCore (Expo 57) exposes OnViewDidUpdateProps as the only
    // view lifecycle hook; OnViewDestroys does not exist and fails xcodebuild.
    expect(swiftSource).not.toContain("OnViewDestroys");
  });

  test("keeps iOS 16.4 baseline with availability-guarded first-frame readiness", () => {
    expect(podspec).toContain(":ios => '16.4'");
    // AVSampleBufferDisplayLayer.isReadyForDisplay requires iOS 17.4+.
    expect(swiftSource).toContain("#available(iOS 17.4");
    expect(swiftSource).not.toContain("observe(\\.isReadyForDisplay");
  });

  test("derives connected from genuine renderer state, not enqueue alone", () => {
    expect(swiftSource).toContain("video.status != .rendering");
    // Exactly one connected report site: the readiness evaluator.
    expect(swiftSource.split('state("connected")').length - 1).toBe(1);
  });

  test("tears down remote control on view removal and deallocation", () => {
    expect(swiftSource).toMatch(
      /override func didMoveToWindow\(\)[\s\S]*?stop\(\)/,
    );
    expect(swiftSource).toMatch(/deinit \{[\s\S]*?stop\(\)/);
  });
});
