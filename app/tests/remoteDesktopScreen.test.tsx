import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

if (!process.env.ZEN_DESKTOP_SCREEN_CHILD) {
  test("mounted desktop route admission and native event lifecycle", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, ZEN_DESKTOP_SCREEN_CHILD: "1" },
    });
    if (result.exitCode) throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const host = (name: string) => (props: any) => React.createElement(name, props, props.children);
  const calls: object[] = [];
  const disconnected: string[] = [];
  let background: (state: string) => void = () => {};
  let current = { id: "fixture", name: "Fixture", url: "ws://192.0.2.1:9876/ws", daemonId: "identity", daemonPublicKey: "key" };
  let prepare: (generation: string) => Promise<string>;
  mock.module("react-native", () => ({
    Alert: { alert() {} }, AppState: { addEventListener: (_: string, callback: typeof background) => {
      background = callback; return { remove() {} };
    } },
    KeyboardAvoidingView: host("keyboard-avoid"), PanResponder: { create: () => ({ panHandlers: {} }) },
    Platform: { OS: "android" }, Pressable: host("button"), Text: host("text"), TextInput: host("input"), View: host("view"),
    StyleSheet: { create: (value: unknown) => value, hairlineWidth: 1, absoluteFill: {} },
  }));
  mock.module("@expo/vector-icons", () => ({ Ionicons: host("icon") }));
  mock.module("expo-router", () => ({ Stack: { Screen: host("screen") }, router: { push() {} },
    useFocusEffect: (effect: () => (() => void)) => React.useEffect(effect, [effect]),
  }));
  mock.module("react-native-safe-area-context", () => ({ SafeAreaView: host("safe-area") }));
  mock.module("../store/currentServer", () => ({ useCurrentServer: () => ({ currentServer: current, isCurrentServer: (id: string) => id === current.id }) }));
  mock.module("../constants/tokens", () => ({ useAppColors: () => ({}) }));
  mock.module("../services/remoteDesktop", () => ({ prepareDesktopConnection: (_: unknown, generation: string) => prepare(generation) }));
  mock.module("../modules/zen-remote-desktop/src", () => ({
    NativeDesktopView: React.forwardRef((props: any, ref) => {
      React.useImperativeHandle(ref, () => ({
        sendCommand: async (_: string, __: number, payload: string) => { calls.push(JSON.parse(payload)); return true; },
        disconnect: async (generation: string) => { disconnected.push(generation); },
        showSensitiveInput: async () => true,
      }));
      return React.createElement("native-desktop", props);
    }),
  }));
  const { default: RemoteDesktopScreen } = await import("../app/remote-desktop");
  const { DesktopPreflightError } = await import("../services/desktopConnectionCheck");
  const ready = async (generation: string) => JSON.stringify({ mode: "unattended", transport: "pinned-link", inputGeneration: generation });
  const mount = async () => {
    calls.length = 0; disconnected.length = 0; prepare = ready;
    let tree!: TestRenderer.ReactTestRenderer;
    await act(async () => { tree = TestRenderer.create(<RemoteDesktopScreen />); });
    return tree;
  };
  const connect = (tree: TestRenderer.ReactTestRenderer) => act(async () => {
    tree.root.findAllByType("button" as any).find((button) => button.findAllByType("text" as any).some((text) => text.children.includes("Connect")))!.props.onPress();
  });
  const event = (tree: TestRenderer.ReactTestRenderer) => tree.root.findByType("native-desktop" as any).props.onState;
  const tool = (tree: TestRenderer.ReactTestRenderer, label: string) => tree.root.findAllByType("button" as any).find((button) => button.props.accessibilityLabel === label)!;

  test("unattended source inventory fails closed without granting control or sending start", async () => {
    const tree = await mount();
    await connect(tree);
    const callback = event(tree);
    await act(async () => callback({ nativeEvent: { state: "sources", source: "x11", control: true } }));
    expect(calls).toEqual([]);
    expect(disconnected.length).toBe(1);
    expect(tree.root.findAllByType("native-desktop" as any)).toHaveLength(0);
    expect(JSON.stringify(tree.toJSON())).toContain("This server requested attended sharing");
    await act(async () => callback({ nativeEvent: { state: "connected", control: true, sensitiveInput: true } }));
    expect(tool(tree, "Keyboard").props.disabled).toBe(true);
    await act(async () => tree.unmount());
  });

  test("scope refusal never mounts native; successful native presentation owns tools until stop", async () => {
    const tree = await mount();
    prepare = async () => { throw new DesktopPreflightError("desktop_scope_required", "This phone has terminal access only."); };
    await connect(tree);
    expect(tree.root.findAllByType("native-desktop" as any)).toHaveLength(0);
    expect(JSON.stringify(tree.toJSON())).toContain("Grant unattended desktop");
    prepare = ready;
    await connect(tree);
    const callback = event(tree);
    await act(async () => callback({ nativeEvent: { state: "streaming", control: true } }));
    expect(tool(tree, "Keyboard").props.disabled).toBe(true);
    await act(async () => callback({ nativeEvent: { state: "connected", control: true, sensitiveInput: true, surface: "locked" } }));
    expect(tool(tree, "Keyboard").props.disabled).toBe(true);
    expect(tool(tree, "OS password").props.disabled).toBe(false);
    await act(async () => background("background"));
    expect(tree.root.findAllByType("native-desktop" as any)).toHaveLength(0);
    expect(tool(tree, "OS password").props.disabled).toBe(true);
    expect(calls).toEqual([]);
    await act(async () => tree.unmount());
  });

  test("cancelled preflight and old-server callbacks cannot mount or re-enable native", async () => {
    const tree = await mount();
    let resolve!: (value: string) => void;
    prepare = () => new Promise((done) => { resolve = done; });
    await connect(tree);
    await act(async () => tool(tree, "Stop desktop").props.onPress());
    await act(async () => resolve(await ready("old")));
    expect(tree.root.findAllByType("native-desktop" as any)).toHaveLength(0);
    prepare = ready;
    await connect(tree);
    const callback = event(tree);
    current = { ...current, id: "second", name: "Second" };
    await act(async () => tree.update(<RemoteDesktopScreen />));
    await act(async () => callback({ nativeEvent: { state: "connected", control: true } }));
    expect(tool(tree, "Keyboard").props.disabled).toBe(true);
    expect(calls).toEqual([]);
    await act(async () => tree.unmount());
  });
}
