import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

describe("structured chat keyboard overlay", () => {
  test("keeps the timeline fixed and moves one absolute Composer overlay", () => {
    const frame = source("CodexChatKeyboardFrame.tsx");

    expect(frame).not.toContain("KeyboardAvoidingView");
    expect(frame).toContain("styles.timelineCanvas");
    expect(frame).toContain("styles.stickyOverlay");
    expect(frame).toContain("position: \"absolute\"");
    expect(frame).toContain("structuredChatOverlayTranslateY(");
    expect(frame).toContain("useStructuredChatWindowMode(enabled)");
  });

  test("represents keyboard and overlay obstruction as non-lifting content inset", () => {
    const timeline = source("CodexTimelineView.tsx");
    const inset = source("StructuredChatInsetScrollView.tsx");

    expect(timeline).not.toContain("KeyboardChatScrollView");
    expect(timeline).toContain("StructuredChatInsetScrollView");
    expect(timeline).toContain("clearance={extraContentPadding}");
    expect(inset).not.toContain("useKeyboardHandler");
    expect(inset).not.toContain("useExtraContentPadding");
    expect(inset).not.toMatch(/\bscrollTo\s*\(/);
    expect(inset).not.toContain("contentOffset:");
    expect(inset).toContain("structuredChatEffectiveClearance");
    expect(inset).toContain("onLatestOffsetChange");
    expect(timeline).not.toContain("TIMELINE_BOTTOM_PADDING");
    expect(timeline).not.toContain("latestContentPadding=");
    expect(inset).not.toContain("latestContentPadding");
    expect(inset).toContain("paddingTop: inverted\n      ? effectiveClearance.value");
    expect(inset).toContain(
      "<Reanimated.View style={[styles.webContent, webContentInsetStyle]}",
    );
    expect(inset).not.toMatch(/height:\s*effectiveClearance\.value/);
    expect(timeline).toContain('Platform.OS === "ios" ? "interactive" : "on-drag"');
  });

  test("measures the complete continuously mounted overlay outside timeline content", () => {
    const body = source("CodexChatBody.tsx");
    const frame = source("CodexChatKeyboardFrame.tsx");

    expect(body).toContain("<TerminalActionPromptCard");
    expect(body).toContain("{composerAccessory}");
    expect(body).toContain("<CodexChatComposerSection");
    expect(body).toContain("composer={composer}");
    expect(body).toContain("portal={skillsSheet}");
    expect(frame).toContain("onLayout={handleComposerLayout}");
    expect(frame).toContain("renderTimeline(scrollClearance)");
    expect(frame).toContain("structuredChatScrollClearance(");
  });

  test("Composer geometry no longer scrolls or pins the timeline", () => {
    const bodyProps = source("useCodexChatBodyProps.ts");
    const body = source("CodexChatBody.tsx");

    expect(bodyProps).not.toContain("handleComposerHeightChange");
    expect(body).not.toContain("onComposerHeightChange");
  });

  test("keeps resize globally for non-chat flows and leases adjustNothing only to Chat", () => {
    const appConfig = source("../../app.base.json");
    const mode = source("chatKeyboardWindowMode.ts");
    const calendar = source("../../app/calendar.tsx");
    const work = source("../../app/work/[id].tsx");

    expect(appConfig).toContain('"softwareKeyboardLayoutMode": "resize"');
    expect(mode).toContain("SOFT_INPUT_ADJUST_NOTHING");
    expect(mode).toContain("KeyboardController.setDefaultMode()");
    expect(calendar).toContain("KeyboardAvoidingView");
    expect(work).toContain("KeyboardAvoidingView");
  });

  test("masks timeline alpha without painting a Composer-side background fade", () => {
    const frame = source("CodexChatKeyboardFrame.tsx");
    const contentFade = source("StructuredChatContentFade.tsx");
    const topFade = frame.indexOf("styles.topFade");

    expect(frame).toContain('pointerEvents="none"');
    expect(frame).toContain("styles.topFade");
    expect(topFade).toBeGreaterThan(-1);
    expect(frame).not.toContain("bottomFade");
    expect(frame).not.toContain("styles.bottomFade");
    expect(frame).toContain("<StructuredChatContentFade");
    expect(frame).toContain("composerHeight={composerHeight}");
    expect(frame).toContain("overlayTranslateY={overlayTranslateY}");
    expect(contentFade).toContain("<MaskedView");
    expect(contentFade).toContain("maskElement={");
    expect(contentFade).toContain('androidRenderingMode="software"');
    expect(contentFade).toContain('pointerEvents="none"');
    expect(contentFade).toContain("maskImage");
    expect(contentFade).toContain("structuredChatContentFadeGeometry(");
    expect(contentFade).not.toContain("TerminalThemeChrome");
    expect(contentFade).not.toContain("appBackground");
    expect(contentFade).not.toContain("canvasColor");
  });

  test("exposes one accessible native action per Composer slot on Web", () => {
    expect(source("ComposerIconButton.tsx")).not.toContain(
      'from "react-native-gesture-handler"',
    );
    expect(source("ComposerSendButton.tsx")).not.toContain(
      'from "react-native-gesture-handler"',
    );
    expect(source("CodexTimelineActivityHeader.tsx")).not.toContain(
      'from "react-native-gesture-handler"',
    );
  });

  test("keeps primary and structured Agent Chat headers in overlay layout", () => {
    const primaryShell = source("../navigation/PrimaryDrawerShell.tsx");
    const terminalLayout = source("../../app/terminal/TerminalScreenLayout.tsx");

    expect(primaryShell).toContain("styles.appBarOverlay,");
    expect(primaryShell).not.toContain('floating={activePrimaryRoute === "brain"}');
    expect(primaryShell).not.toContain("floating ? styles.appBarFloating : null");
    expect(terminalLayout).toContain(
      "const floatingChatChrome = viewportProps.showCodexChat",
    );
    expect(terminalLayout.indexOf("<TerminalTopBar {...topBarProps}"))
      .toBeLessThan(terminalLayout.indexOf("<TerminalViewport"));
  });

  test("grows the production multiline input from wrapped content", () => {
    const input = source("CodexComposerInput.tsx");
    const demo = source("../../app/screenshot-demo.tsx");

    expect(input).toContain("onContentSizeChange={handleContentSizeChange}");
    expect(input).toContain("MAX_INPUT_HEIGHT");
    expect(input).toContain("height: inputHeight");
    expect(demo).toContain(
      "keyboard overlay.\\nPreserve the visible message anchor.\\nVerify every narrow width.",
    );
  });

  test("keeps floating Agent navigation first in accessibility traversal", () => {
    const terminalLayout = source("../../app/terminal/TerminalScreenLayout.tsx");

    expect(terminalLayout.indexOf("styles.headerOverlay"))
      .toBeLessThan(terminalLayout.indexOf("<TerminalViewport"));
    expect(terminalLayout.indexOf("<TerminalTopBar {...topBarProps}"))
      .toBeLessThan(terminalLayout.indexOf("<TerminalViewport"));
  });

  test("uses the production fixed canvas for the Brain fixture", () => {
    const demo = source("../../app/screenshot-demo.tsx");
    const brain = demo.slice(
      demo.indexOf("function BrainDemo()"),
      demo.indexOf("function SessionsDemo()"),
    );

    expect(brain).toContain("<CodexChatKeyboardFrame");
    expect(brain).toContain("<CodexTimelineView");
    expect(brain).toContain("<CodexChatComposer");
    expect(brain).toContain("topChromeInset={topChromeInset}");
    expect(brain).not.toContain("<DemoTimeline");
    expect(brain).not.toContain("<DemoComposer");
  });

  test("the isolated Web fixture renders production timeline and Composer primitives", () => {
    const demo = source("../../app/screenshot-demo.tsx");

    expect(demo).toContain("<CodexChatKeyboardFrame");
    expect(demo).toContain("<CodexTimelineView");
    expect(demo).toContain("<CodexChatComposer");
    expect(demo).toContain("useCodexTimelineItems({");
    expect(demo).toContain("workingTurn,");
    expect(demo).toContain("showAttachmentRail");
    expect(demo).toContain("showStopButton={working && !hasContent}");
    expect(demo).toContain("onLatestOffsetChange={handleLatestOffsetChange}");
  });
});
