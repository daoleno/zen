import { describe, expect, test } from "bun:test";
import {
  mkdtempSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import {
  NOTICE_BUNDLE_REL,
  NOTICE_SRC_REL,
  applyZenIOSPodfileProperties,
  applyZenIOSVersionBuildSettings,
  copyGhosttyNoticeIntoIOSProject,
  linkGhosttyNoticeResource,
} from "./withZenIOSBuild.js";

describe("withZenIOSBuild Podfile properties", () => {
  test("disables ABI-mismatched precompiled Expo modules", () => {
    const properties = applyZenIOSPodfileProperties({
      "expo.jsEngine": "hermes",
    });

    expect(properties).toEqual({
      "expo.jsEngine": "hermes",
      EXPO_USE_PRECOMPILED_MODULES: "false",
    });
  });

  test("overrides stale generated values", () => {
    expect(
      applyZenIOSPodfileProperties({ EXPO_USE_PRECOMPILED_MODULES: "true" })
        .EXPO_USE_PRECOMPILED_MODULES,
    ).toBe("false");
  });
});

describe("withZenIOSBuild native version contract", () => {
  test("sets every generated Xcode configuration to the packaged iOS values", () => {
    const configurations = {
      DEBUG: {
        buildSettings: {
          MARKETING_VERSION: "1.0",
          CURRENT_PROJECT_VERSION: "1",
        },
      },
      DEBUG_comment: "Debug",
      RELEASE: { buildSettings: {} },
    };
    const project = {
      pbxXCBuildConfigurationSection: () => configurations,
    };

    applyZenIOSVersionBuildSettings(project, {
      marketingVersion: "0.1.0",
      buildNumber: "42",
    });

    expect(configurations.DEBUG.buildSettings.MARKETING_VERSION).toBe("0.1.0");
    expect(configurations.DEBUG.buildSettings.CURRENT_PROJECT_VERSION).toBe(
      "42",
    );
    expect(configurations.RELEASE.buildSettings.MARKETING_VERSION).toBe(
      "0.1.0",
    );
    expect(configurations.RELEASE.buildSettings.CURRENT_PROJECT_VERSION).toBe(
      "42",
    );
  });

  test("rejects App Store-invalid marketing and build values", () => {
    const project = { pbxXCBuildConfigurationSection: () => ({}) };
    expect(() =>
      applyZenIOSVersionBuildSettings(project, {
        marketingVersion: "0.1.0-beta.2",
        buildNumber: "42",
      }),
    ).toThrow("invalid iOS marketing version");
    expect(() =>
      applyZenIOSVersionBuildSettings(project, {
        marketingVersion: "0.1.0",
        buildNumber: "0",
      }),
    ).toThrow("invalid iOS build number");
  });
});

describe("withZenIOSBuild Ghostty MIT notice packaging", () => {
  test("copies the tracked notice into the iOS app project root for flatten-safe packaging", () => {
    const root = mkdtempSync(
      join(process.env.TMPDIR || process.cwd(), "zen-ios-notice-"),
    );
    try {
      const projectRoot = join(root, "app");
      const platformProjectRoot = join(projectRoot, "ios");
      const projectName = "Zen";
      mkdirSync(join(projectRoot, "assets", "notices"), { recursive: true });
      mkdirSync(join(platformProjectRoot, projectName), { recursive: true });
      writeFileSync(
        join(projectRoot, NOTICE_SRC_REL),
        "MIT License\nCopyright (c) Ghostty\n",
      );

      const result = copyGhosttyNoticeIntoIOSProject(
        projectRoot,
        platformProjectRoot,
        projectName,
      );
      expect(result).toEqual({
        projectName,
        bundleRelativePath: NOTICE_BUNDLE_REL,
        projectRelativePath: join(projectName, NOTICE_BUNDLE_REL),
      });
      expect(
        readFileSync(
          join(platformProjectRoot, projectName, NOTICE_BUNDLE_REL),
          "utf8",
        ),
      ).toContain("MIT License");
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  test("skips relinking when the notice is already in the Xcode project", () => {
    const project = {
      hasFile: () => true,
    };
    expect(
      linkGhosttyNoticeResource(project, {
        projectName: "Zen",
        projectRelativePath: "Zen/GHOSTTY-MIT.txt",
      }),
    ).toBe(project);
  });

  test("keeps the iOS bundle notice path aligned with native.lock.json", () => {
    const lock = JSON.parse(
      readFileSync(
        join(__dirname, "..", "modules", "zen-terminal-vt", "native.lock.json"),
        "utf8",
      ),
    );
    expect(NOTICE_BUNDLE_REL).toBe(lock.ios.notice_bundle_path);
    expect(NOTICE_SRC_REL).toBe(lock.ios.notice_source.replace(/^app\//, ""));
  });
});
