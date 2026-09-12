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
  const pushes: any[] = [];
  const disconnected: string[] = [];
  let background: (state: string) => void = () => {};
  const defaultServer = { id: "fixture", name: "Fixture", url: "ws://192.0.2.1:9876/ws", daemonId: "identity", daemonPublicKey: "key" };
  let current = { ...defaultServer };
  let prepare: (generation: string) => Promise<string>;
  let confirmResult = true;
  let confirmImpl: (() => Promise<boolean>) | null = null;
  let enable: (server: unknown, signal?: AbortSignal) => Promise<void> = async () => {};
  mock.module("react-native", () => ({
    Alert: { alert() {} }, AppState: { addEventListener: (_: string, callback: typeof background) => {
      background = callback; return { remove() {} };
    } },
    KeyboardAvoidingView: host("keyboard-avoid"), PanResponder: { create: () => ({ panHandlers: {} }) },
    Platform: { OS: "android" }, Pressable: host("button"), Text: host("text"), TextInput: host("input"), View: host("view"),
    StyleSheet: { create: (value: unknown) => value, hairlineWidth: 1, absoluteFill: {} },
  }));
  mock.module("@expo/vector-icons", () => ({ Ionicons: host("icon") }));
  mock.module("expo-router", () => ({ Stack: { Screen: host("screen") }, router: { push: (value: unknown) => { pushes.push(value); } },
    useFocusEffect: (effect: () => (() => void)) => React.useEffect(effect, [effect]),
  }));
  mock.module("react-native-safe-area-context", () => ({ SafeAreaView: host("safe-area") }));
  mock.module("../store/currentServer", () => ({ useCurrentServer: () => ({ currentServer: current, isCurrentServer: (id: string) => id === current.id }) }));
  mock.module("../constants/tokens", () => ({ useAppColors: () => ({}) }));
  mock.module("../services/remoteDesktop", () => ({ prepareDesktopConnection: (_: unknown, generation: string) => prepare(generation) }));
  mock.module("../services/desktopScopeGrant", () => ({ enableDesktopScope: (server: unknown, signal?: AbortSignal) => enable(server, signal) }));
  mock.module("../services/confirmDesktopEnable", () => ({ confirmDesktopEnable: () => confirmImpl ? confirmImpl() : Promise.resolve(confirmResult) }));
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
    current = { ...defaultServer };
    calls.length = 0; disconnected.length = 0; pushes.length = 0; prepare = ready; confirmResult = true; confirmImpl = null; enable = async () => {};
    let tree!: TestRenderer.ReactTestRenderer;
    await act(async () => { tree = TestRenderer.create(<RemoteDesktopScreen />); });
    return tree;
  };
  const connect = (tree: TestRenderer.ReactTestRenderer) => act(async () => {
    tree.root.findAllByType("button" as any).find((button) => button.findAllByType("text" as any).some((text) => text.children.includes("Connect")))!.props.onPress();
  });
  const button = (tree: TestRenderer.ReactTestRenderer, label: string) => tree.root.findAllByType("button" as any)
    .find((candidate) => candidate.findAllByType("text" as any).some((text) => text.children.includes(label)));
  const press = (tree: TestRenderer.ReactTestRenderer, label: string) => act(async () => { button(tree, label)!.props.onPress(); });
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

  test("scope0 offers in-place enable; cancel changes nothing and confirm connects without scanner", async () => {
    const tree = await mount();
    prepare = async () => { throw new DesktopPreflightError("desktop_scope_required", "Enable remote desktop in the Zen app on this phone."); };
    await connect(tree);
    expect(tree.root.findAllByType("native-desktop" as any)).toHaveLength(0);
    expect(JSON.stringify(tree.toJSON())).toContain("Enable remote desktop");
    expect(button(tree, "Connect")).toBeUndefined();
    let grants = 0;
    enable = async () => { grants++; };
    confirmResult = false;
    await press(tree, "Enable remote desktop");
    expect(grants).toBe(0);
    expect(pushes).toEqual([]);
    expect(tree.root.findAllByType("native-desktop" as any)).toHaveLength(0);
    confirmResult = true;
    prepare = ready;
    await press(tree, "Enable remote desktop");
    expect(grants).toBe(1);
    expect(pushes).toEqual([]);
    expect(tree.root.findAllByType("native-desktop" as any)).toHaveLength(1);
    await act(async () => tree.unmount());
  });

  test("failed in-place enable keeps retry clear and a later success connects", async () => {
    const tree = await mount();
    prepare = async () => { throw new DesktopPreflightError("desktop_scope_required", "Enable remote desktop in the Zen app on this phone."); };
    await connect(tree);
    enable = async () => { throw new Error("Could not reach this computer to enable remote desktop."); };
    await press(tree, "Enable remote desktop");
    expect(JSON.stringify(tree.toJSON())).toContain("Could not reach this computer");
    expect(button(tree, "Enable remote desktop")).toBeDefined();
    expect(tree.root.findAllByType("native-desktop" as any)).toHaveLength(0);
    prepare = ready;
    enable = async () => {};
    await press(tree, "Enable remote desktop");
    expect(tree.root.findAllByType("native-desktop" as any)).toHaveLength(1);
    await act(async () => tree.unmount());
  });

  test("insecure transport refusal shows recovery with no grant attempt", async () => {
    const tree = await mount();
    prepare = async () => { throw new DesktopPreflightError("desktop_scope_required", "Enable remote desktop in the Zen app on this phone."); };
    await connect(tree);
    enable = async () => { throw new Error("Remote desktop requires this computer's identity-bound encrypted connection."); };
    await press(tree, "Enable remote desktop");
    expect(JSON.stringify(tree.toJSON())).toContain("identity-bound encrypted connection");
    expect(tree.root.findAllByType("native-desktop" as any)).toHaveLength(0);
    await act(async () => tree.unmount());
  });

  test("server change while confirmation is open does not grant or connect", async () => {
    const tree = await mount();
    prepare = async () => { throw new DesktopPreflightError("desktop_scope_required", "Enable remote desktop in the Zen app on this phone."); };
    await connect(tree);
    let resolveConfirm!: (value: boolean) => void;
    confirmImpl = () => new Promise<boolean>((resolve) => { resolveConfirm = resolve; });
    let grants = 0;
    enable = async () => { grants++; };
    await press(tree, "Enable remote desktop");
    current = { ...current, id: "second", name: "Second" };
    await act(async () => tree.update(<RemoteDesktopScreen />));
    await act(async () => resolveConfirm(true));
    expect(grants).toBe(0);
    expect(tree.root.findAllByType("native-desktop" as any)).toHaveLength(0);
    await act(async () => tree.unmount());
  });

  test("stale enable cannot connect after server change", async () => {
    const tree = await mount();
    prepare = async () => { throw new DesktopPreflightError("desktop_scope_required", "Enable remote desktop in the Zen app on this phone."); };
    await connect(tree);
    let resolveGrant!: () => void;
    enable = () => new Promise<void>((resolve) => { resolveGrant = resolve; });
    prepare = ready;
    await press(tree, "Enable remote desktop");
    current = { ...current, id: "second", name: "Second" };
    await act(async () => tree.update(<RemoteDesktopScreen />));
    await act(async () => resolveGrant());
    expect(tree.root.findAllByType("native-desktop" as any)).toHaveLength(0);
    expect(pushes).toEqual([]);
    await act(async () => tree.unmount());
  });

  test("revoked device offers real pairing instead of in-place enable", async () => {
    const tree = await mount();
    prepare = async () => { throw new DesktopPreflightError("device_revoked", "This device is no longer paired with the computer."); };
    await connect(tree);
    expect(button(tree, "Enable remote desktop")).toBeUndefined();
    await press(tree, "Pair this phone");
    const pushed = pushes.at(-1) as any;
    expect(pushed.pathname).toBe("/settings");
    expect(pushed.params.pairMode).toBe("scanner");
    expect(typeof pushed.params.addServer).toBe("string");
    await act(async () => tree.unmount());
  });

  test("successful native presentation owns tools until stop", async () => {
    const tree = await mount();
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
