import { afterAll, describe, expect, mock, test } from "bun:test";
import { join } from "node:path";
import React from "react";
import type { SharedValue } from "react-native-reanimated";
import type { StructuredChatKeyboardLifecycleGate } from "./chatKeyboardOverlayPolicy";

type TestPlatform = "android" | "ios" | "web";
type TestElement = React.ReactElement<
  Record<string, unknown>,
  React.ElementType
>;

const ISOLATED_RUN_ENV = "ZEN_STRUCTURED_CHAT_CONTENT_FADE_TEST";
const isolatedRun = process.env[ISOLATED_RUN_ENV] === "1";

let platformOS: TestPlatform = "android";

const ViewMarker = () => null;
const LinearGradientMarker = () => null;
const AnimatedLinearGradientMarker = () => null;
const AnimatedViewMarker = () => null;
const MaskedViewMarker = () => null;
const TimelineMarker = () => null;

const platform = {} as { readonly OS: TestPlatform };
Object.defineProperty(platform, "OS", {
  get: () => platformOS,
});
const timeline = React.createElement(TimelineMarker, { id: "timeline" });
let StructuredChatContentFade: (typeof import("./StructuredChatContentFade"))["StructuredChatContentFade"];

if (!isolatedRun) {
  test("executes platform routing in an isolated Bun module graph", () => {
    // Bun 1.3.5 cannot re-mock react-native after another suite restores it.
    // A fresh module graph keeps this mock-backed production import isolated.
    const result = Bun.spawnSync({
      cmd: [process.execPath, "test", import.meta.path],
      cwd: join(import.meta.dir, "../.."),
      env: {
        ...process.env,
        [ISOLATED_RUN_ENV]: "1",
      },
      stdout: "pipe",
      stderr: "pipe",
    });
    const output = [result.stdout, result.stderr]
      .map((bytes) => new TextDecoder().decode(bytes))
      .join("\n");

    if (result.exitCode !== 0) {
      throw new Error(output);
    }
    expect(output).toContain("3 pass");
  });
} else {
  mock.module("react-native", () => ({
    Platform: platform,
    StyleSheet: {
      create: <Styles>(styles: Styles) => styles,
    },
    View: ViewMarker,
  }));

  mock.module("expo-linear-gradient", () => ({
    LinearGradient: LinearGradientMarker,
  }));

  mock.module("@react-native-masked-view/masked-view", () => ({
    default: MaskedViewMarker,
  }));

  mock.module("react-native-reanimated", () => ({
    default: {
      View: AnimatedViewMarker,
      createAnimatedComponent: () => AnimatedLinearGradientMarker,
    },
    useAnimatedStyle: (worklet: () => unknown) => worklet(),
  }));

  const contentFadeModule =
    require("./StructuredChatContentFade") as typeof import("./StructuredChatContentFade");
  ({ StructuredChatContentFade } = contentFadeModule);

  afterAll(() => {
    mock.restore();
  });
}

const sharedValue = (value: number) => ({ value }) as SharedValue<number>;

function keyboardGate(
  overlayTranslateY: number,
): SharedValue<StructuredChatKeyboardLifecycleGate> {
  const open = overlayTranslateY < 0;
  return {
    value: {
      enabled: true,
      appActive: true,
      composerFocused: open,
      revision: 1,
      authoritativeRevision: open ? 1 : 0,
      nativeImeVisible: open,
      nativeComposerFocused: open,
      forceLifecycleContractionRevision: 0,
      keyboardTranslation: overlayTranslateY,
      keyboardProgress: open ? 1 : 0,
    },
  } as SharedValue<StructuredChatKeyboardLifecycleGate>;
}

function renderContentFade(
  os: TestPlatform,
  composerHeight: number,
  overlayTranslateY: number,
) {
  platformOS = os;
  const routed = StructuredChatContentFade({
    canvasColor: "#0F0F14",
    composerHeight: sharedValue(composerHeight),
    keyboardLifecycleGate: keyboardGate(overlayTranslateY),
    keyboardVerticalOffset: 0,
    children: timeline,
  }) as TestElement;
  const RoutedComponent = routed.type as (
    props: Record<string, unknown>,
  ) => TestElement;

  return {
    routed,
    rendered: RoutedComponent(routed.props),
  };
}

function childrenOf(element: TestElement) {
  const { children } = element.props;
  return (Array.isArray(children) ? children : [children]) as TestElement[];
}

function elementTypes(element: TestElement): React.ElementType[] {
  return [
    element.type,
    ...React.Children.toArray(
      element.props.children as React.ReactNode,
    ).flatMap((child) =>
      React.isValidElement<Record<string, unknown>>(child)
        ? elementTypes(child as TestElement)
        : [],
    ),
  ];
}

function describeIsolated(name: string, body: () => void) {
  if (isolatedRun) {
    describe(name, body);
  }
}

describeIsolated("StructuredChatContentFade routing", () => {
  test("renders Android timeline, fade, and canvas cover as ordered siblings", () => {
    for (const [composerHeight, overlayTranslateY, expected] of [
      [100, 0, { transparentBottomInset: 10, fadeHeight: 40 }],
      [100, -20, { transparentBottomInset: 30, fadeHeight: 40 }],
      [188, -300, { transparentBottomInset: 318.8, fadeHeight: 75.2 }],
    ] as const) {
      const { routed, rendered } = renderContentFade(
        "android",
        composerHeight,
        overlayTranslateY,
      );
      const [renderedTimeline, gradient, cover] = childrenOf(rendered);
      const gradientStyles = gradient.props.style as Record<string, number>[];
      const coverStyles = cover.props.style as Array<Record<string, unknown>>;

      expect((routed.type as Function).name).toBe(
        "AndroidStructuredChatContentFade",
      );
      expect(rendered.type).toBe(ViewMarker);
      expect([renderedTimeline.type, gradient.type, cover.type]).toEqual([
        TimelineMarker,
        AnimatedLinearGradientMarker,
        AnimatedViewMarker,
      ]);
      expect(elementTypes(rendered)).not.toContain(MaskedViewMarker);
      expect(renderedTimeline).toBe(timeline);
      expect(gradient.props.pointerEvents).toBe("none");
      expect(cover.props.pointerEvents).toBe("none");
      expect(gradient.props.colors).toEqual([
        "rgba(15, 15, 20, 0)",
        "rgba(15, 15, 20, 1)",
      ]);
      expect(gradientStyles[1]?.bottom).toBeCloseTo(
        expected.transparentBottomInset,
      );
      expect(gradientStyles[1]?.height).toBeCloseTo(expected.fadeHeight);
      expect(coverStyles[1]?.height).toBeCloseTo(
        expected.transparentBottomInset,
      );
      expect(coverStyles[0]).toEqual({
        position: "absolute",
        right: 0,
        bottom: 0,
        left: 0,
      });
      expect(coverStyles[2]).toEqual({ backgroundColor: "#0F0F14" });
    }
  });

  test("routes iOS through the native mask with the timeline as its child", () => {
    const { routed, rendered } = renderContentFade("ios", 100, -20);
    const mask = rendered.props.maskElement as TestElement;
    const [opaqueMask, gradient] = childrenOf(mask);
    const opaqueStyles = opaqueMask.props.style as Array<
      Record<string, unknown>
    >;
    const gradientStyles = gradient.props.style as Record<string, number>[];

    expect((routed.type as Function).name).toBe("IosStructuredChatContentFade");
    expect(rendered.type).toBe(MaskedViewMarker);
    expect(rendered.props.children).toBe(timeline);
    expect(rendered.props).not.toHaveProperty("androidRenderingMode");
    expect(mask.type).toBe(ViewMarker);
    expect(mask.props.pointerEvents).toBe("none");
    expect(gradient.type).toBe(AnimatedLinearGradientMarker);
    expect(gradient.props.pointerEvents).toBe("none");
    expect(gradient.props.colors).toEqual([
      "rgba(15, 15, 20, 1)",
      "rgba(15, 15, 20, 0)",
    ]);
    expect(opaqueStyles[1]).toEqual({ bottom: 70 });
    expect(opaqueStyles[2]).toEqual({
      backgroundColor: "rgba(15, 15, 20, 1)",
    });
    expect(gradientStyles[1]?.bottom).toBeCloseTo(30);
    expect(gradientStyles[1]?.height).toBeCloseTo(40);
  });

  test("routes Web through paired CSS alpha-mask properties", () => {
    const { routed, rendered } = renderContentFade("web", 100, -20);
    const styles = rendered.props.style as Array<Record<string, unknown>>;
    const maskStyle = styles[1];
    const maskImage = [
      "linear-gradient(to bottom,",
      "rgba(255, 255, 255, 1) 0,",
      "rgba(255, 255, 255, 1) calc(100% - 70px),",
      "rgba(255, 255, 255, 0) calc(100% - 30px),",
      "rgba(255, 255, 255, 0) 100%)",
    ].join(" ");

    expect((routed.type as Function).name).toBe("WebStructuredChatContentFade");
    expect(rendered.type).toBe(AnimatedViewMarker);
    expect(rendered.props.children).toBe(timeline);
    expect(maskStyle).toEqual({
      maskImage,
      WebkitMaskImage: maskImage,
      maskMode: "alpha",
      WebkitMaskMode: "alpha",
      maskRepeat: "no-repeat",
      WebkitMaskRepeat: "no-repeat",
      maskSize: "100% 100%",
      WebkitMaskSize: "100% 100%",
    });
  });
});
