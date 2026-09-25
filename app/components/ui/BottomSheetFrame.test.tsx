import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
const host = (name: string) => (props: any) =>
  React.createElement(name, props, props.children);
const modal = (props: any) =>
  React.createElement("modal", props, props.visible ? props.children : null);

mock.module("react-native", () => ({
  KeyboardAvoidingView: host("keyboard"),
  Modal: modal,
  Platform: { OS: "ios" },
  Pressable: host("pressable"),
  StyleSheet: {
    create: (value: unknown) => value,
    flatten: (value: unknown) => value,
    absoluteFill: {},
    hairlineWidth: 1,
  },
  View: host("view"),
}));
mock.module("react-native-reanimated", () => ({
  default: {
    View: host("animatedView"),
    createAnimatedComponent: (component: any) => component,
  },
  createAnimatedComponent: (component: any) => component,
  runOnJS: (fn: () => void) => fn,
  useAnimatedStyle: (worklet: () => unknown) => worklet(),
  useSharedValue: (value: unknown) => React.useRef({ value }).current,
  withSpring: (value: unknown) => value,
  withTiming: (value: unknown) => value,
  Easing: { out: (fn: unknown) => fn, ease: () => 1 },
}));
mock.module("react-native-gesture-handler", () => ({
  Gesture: {
    Pan: () => ({
      enabled() {
        return this;
      },
      activeOffsetY() {
        return this;
      },
      failOffsetX() {
        return this;
      },
      onUpdate() {
        return this;
      },
      onEnd() {
        return this;
      },
      onFinalize() {
        return this;
      },
    }),
  },
  GestureDetector: ({ children }: any) => children,
}));
mock.module("../../constants/tokens", () => ({
  Radii: { sheet: 32 },
  useAppColors: () => ({
    modalBackdrop: "black",
    borderStrong: "gray",
  }),
}));
mock.module("react-native-safe-area-context", () => ({
  useSafeAreaInsets: () => ({ top: 0, right: 0, bottom: 0, left: 0 }),
}));
mock.module("./GlassSurface", () => ({ GlassSurface: host("glass") }));
mock.module("../../constants/motion", () => ({ Spring: { rise: {} } }));

const { BottomSheetFrame } = await import("./BottomSheetFrame");

test("closing a bottom sheet removes the modal backdrop and leaves no press target", async () => {
  const onClose = mock(() => {});
  let renderer!: TestRenderer.ReactTestRenderer;
  await act(async () => {
    renderer = TestRenderer.create(
      <BottomSheetFrame visible onClose={onClose}>
        {React.createElement("content")}
      </BottomSheetFrame>,
    );
  });
  expect(renderer.root.findByType("modal" as any).props.visible).toBe(true);
  expect(renderer.root.findAllByType("pressable" as any)).toHaveLength(1);
  await act(async () => {
    renderer.update(
      <BottomSheetFrame visible={false} onClose={onClose}>
        {React.createElement("content")}
      </BottomSheetFrame>,
    );
  });
  expect(renderer.root.findAllByType("modal" as any)).toHaveLength(0);
  expect(renderer.root.findAllByType("pressable" as any)).toHaveLength(0);
});
