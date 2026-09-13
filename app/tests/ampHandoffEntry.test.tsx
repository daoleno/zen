import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

// Keep native module mocks isolated from the rest of the suite.
if (!process.env.ZEN_AMP_HANDOFF_CHILD) {
  test("mounted Amp handoff entry and lifecycle", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, ZEN_AMP_HANDOFF_CHILD: "1" }, timeout: 20_000,
    });
    if (result.exitCode) throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const host = (name: string) => (props: any) => React.createElement(name, props, props.children);
  let write: (command: string) => Promise<boolean> = async () => true;
  mock.module("react-native", () => ({
    ActivityIndicator: host("busy"), ScrollView: host("scroll"), Text: host("text"), View: host("view"),
    StyleSheet: { create: (value: unknown) => value, hairlineWidth: 1 },
  }));
  mock.module("@expo/vector-icons", () => ({ Ionicons: host("icon") }));
  mock.module("expo-clipboard", () => ({ setStringAsync: (command: string) => write(command) }));
  mock.module("react-native-safe-area-context", () => ({ useSafeAreaInsets: () => ({ top: 24, bottom: 34, left: 0, right: 0 }) }));
  mock.module("../constants/tokens", () => ({
    useAppColors: () => ({}), UiTextMetrics: {}, Radii: { sm: 12, md: 16, xs: 8 },
    TypeScale: { body: {}, compact: {}, caption: {}, heading: {}, label: {}, mono: {} },
  }));
  mock.module("../components/ui/AnimatedPressable", () => ({ AnimatedPressable: host("button") }));
  mock.module("../components/ui/RisingSheet", () => ({ RisingSheet: (props: any) => React.createElement("sheet", props, props.visible ? props.children : null) }));
  const { AmpHandoffEntry } = await import("../components/terminal/AmpHandoffEntry");
  const { AMP_EXTERNAL_COMMAND } = await import("../services/ampHandoff");
  const mount = async () => {
    write = async () => true;
    let tree!: TestRenderer.ReactTestRenderer;
    await act(async () => { tree = TestRenderer.create(<AmpHandoffEntry key="server-a" />); });
    return tree;
  };
  const button = (tree: TestRenderer.ReactTestRenderer, label: string) => tree.root.findAllByType("button" as any)
    .find((node) => node.props.accessibilityLabel === label)!;
  const press = (tree: TestRenderer.ReactTestRenderer, label: string) => act(async () => { button(tree, label).props.onPress(); });
  const content = (tree: TestRenderer.ReactTestRenderer) => JSON.stringify(tree.toJSON());

  test("entry opens a truthful sheet offline, with no key input or executable launch", async () => {
    const tree = await mount();
    expect(tree.root.findByType("sheet" as any).props.visible).toBe(false);
    await press(tree, "Amp, External handoff");
    expect(button(tree, "Amp, External handoff").props.accessibilityState.expanded).toBe(true);
    expect(content(tree)).toContain("Not verified in Zen");
    expect(content(tree)).toContain("not unlimited inference or compute");
    expect(content(tree)).toContain("No verified OpenCode Go connection");
    expect(button(tree, "Launch Amp in Zen unavailable").props.disabled).toBe(true);
    expect(button(tree, "Launch Amp in Zen unavailable").props.onPress).toBeUndefined();
    expect(tree.root.findAllByType("input" as any)).toHaveLength(0);
    expect(tree.root.findByType("scroll" as any).props.contentContainerStyle[1].paddingBottom).toBe(34);
    expect(tree.root.findAllByType("text" as any).find((node) => node.props.selectable)!.children).toEqual([AMP_EXTERNAL_COMMAND]);
    await press(tree, "Close Amp handoff");
    expect(tree.root.findByType("sheet" as any).props.visible).toBe(false);
    await act(async () => tree.unmount());
  });

  test("copying has a busy accessible state, ignores duplicate taps and confirms only the clipboard", async () => {
    const tree = await mount();
    await press(tree, "Amp, External handoff");
    let resolve!: (value: boolean) => void;
    const writes: string[] = [];
    write = (command) => { writes.push(command); return new Promise((done) => { resolve = done; }); };
    await act(async () => {
      const onPress = button(tree, "Copy Amp command").props.onPress;
      onPress(); onPress();
    });
    expect(writes).toEqual([AMP_EXTERNAL_COMMAND]);
    expect(button(tree, "Copy Amp command").props.accessibilityState).toEqual({ disabled: true, busy: true });
    expect(content(tree)).toContain("Copying command...");
    await act(async () => resolve(true));
    expect(content(tree)).toContain("Command copied. No session started.");
    expect(button(tree, "Copy Amp command").props.disabled).toBe(false);
    await act(async () => tree.unmount());
  });

  test("clipboard refusal and secret-bearing rejection are redacted, selectable and retryable", async () => {
    const tree = await mount();
    await press(tree, "Amp, External handoff");
    write = async () => false;
    await press(tree, "Copy Amp command");
    expect(content(tree)).toContain("Clipboard unavailable. Command not copied.");
    write = async () => { throw new Error("fixture-private-value-never-render"); };
    await press(tree, "Copy Amp command");
    expect(content(tree)).not.toContain("fixture-private-value-never-render");
    expect(content(tree)).not.toContain("Command copied.");
    expect(tree.root.findAllByType("text" as any).some((node) => node.props.accessibilityRole === "alert")).toBe(true);
    write = async () => true;
    await press(tree, "Copy Amp command");
    expect(content(tree)).toContain("Command copied. No session started.");
    await act(async () => tree.unmount());
  });

  test("dismiss/reopen and current-server remount discard stale copy results", async () => {
    const tree = await mount();
    await press(tree, "Amp, External handoff");
    let resolve!: (value: boolean) => void;
    write = () => new Promise((done) => { resolve = done; });
    await press(tree, "Copy Amp command");
    await act(async () => tree.root.findByType("sheet" as any).props.onClose());
    await press(tree, "Amp, External handoff");
    await act(async () => resolve(true));
    expect(content(tree)).not.toContain("Command copied.");
    expect(button(tree, "Copy Amp command").props.disabled).toBe(false);
    await press(tree, "Copy Amp command");
    await act(async () => tree.update(<AmpHandoffEntry key="server-b" />));
    expect(tree.root.findByType("sheet" as any).props.visible).toBe(false);
    await press(tree, "Amp, External handoff");
    await act(async () => resolve(true));
    expect(content(tree)).not.toContain("Command copied.");
    expect(button(tree, "Copy Amp command").props.disabled).toBe(false);
    await act(async () => tree.unmount());
  });
}
