import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

if (!process.env.ZEN_TELEGRAM_PANEL_CHILD) {
  test("mounted Telegram connection states and actions", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], { env: { ...process.env, ZEN_TELEGRAM_PANEL_CHILD: "1" } });
    if (result.exitCode) throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const host = (name: string) => (props: any) => React.createElement(name, props, props.children);
  mock.module("react-native", () => ({ ActivityIndicator: host("busy"), ScrollView: host("scroll"), Text: host("text"), TextInput: host("input"), View: host("view"), StyleSheet: { create: (x: unknown) => x, hairlineWidth: 1 } }));
  mock.module("@expo/vector-icons", () => ({ Ionicons: host("icon") }));
  mock.module("react-native-safe-area-context", () => ({ useSafeAreaInsets: () => ({ top: 24, bottom: 34, left: 0, right: 0 }) }));
  mock.module("../constants/tokens", () => ({ useAppColors: () => ({}), UiTextMetrics: {}, TypeScale: { body: {}, compact: {}, caption: {}, heading: {} } }));
  mock.module("../components/ui/AnimatedPressable", () => ({ AnimatedPressable: host("button") }));
  const { TelegramConnectionPanel } = await import("../components/settings/TelegramConnectionPanel");
  type Props = React.ComponentProps<typeof TelegramConnectionPanel>;
  const base = (): Props => ({
    status: { state: "connected", enabled: true, binding_pending: false, bot_username: "fixture_bot", owner_hint: "@owner", topics_available: true, brain_topic_id: 42 },
    connected: true, loading: false, busy: false, error: null, token: "", editingToken: false,
    onToken() {}, onPaste() {}, onConfigure() {}, onBind() {}, onOpen() {}, onBotFather() {}, onReconnect() {}, onDisconnect() {}, onEditToken() {}, onCancelToken() {}, onRevoke() {}, onRemove() {}, onRetry() {},
  });
  const mount = async (props: Props) => { let tree!: TestRenderer.ReactTestRenderer; await act(async () => { tree = TestRenderer.create(<TelegramConnectionPanel {...props} />); }); return tree; };
  const button = (tree: TestRenderer.ReactTestRenderer, label: string) => tree.root.findAllByType("button" as any).find(node => node.props.accessibilityLabel === label)!;

  test("connected view keeps diagnostics and destructive actions closed until Advanced", async () => {
    let disconnects = 0;
    const tree = await mount({ ...base(), onDisconnect: () => disconnects++ });
    expect(button(tree, "Disconnect")).toBeDefined();
    expect(button(tree, "Remove bot")).toBeUndefined();
    expect(JSON.stringify(tree.toJSON())).toContain("Brain");
    await act(async () => button(tree, "Disconnect").props.onPress());
    expect(disconnects).toBe(1);
    await act(async () => button(tree, "Advanced").props.onPress());
    expect(button(tree, "Remove bot")).toBeDefined();
    expect(button(tree, "Advanced").props.accessibilityState.expanded).toBe(true);
    await act(async () => tree.unmount());
  });

  test("unconfigured token remains secure and cannot submit blank or busy input", async () => {
    let submitted = 0;
    const props = { ...base(), status: null, onConfigure: () => submitted++ };
    const tree = await mount(props);
    expect(tree.root.findByType("input" as any).props.secureTextEntry).toBe(true);
    expect(button(tree, "Verify token").props.disabled).toBe(true);
    expect(button(tree, "Paste Telegram bot token from clipboard")).toBeDefined();
    await act(async () => tree.update(<TelegramConnectionPanel {...props} token="fixture-only" />));
    expect(button(tree, "Verify token").props.disabled).toBe(false);
    await act(async () => button(tree, "Verify token").props.onPress());
    expect(submitted).toBe(1);
    await act(async () => tree.update(<TelegramConnectionPanel {...props} token="fixture-only" busy />));
    expect(tree.root.findByType("input" as any).props.editable).toBe(false);
    expect(button(tree, "Verifying").props.disabled).toBe(true);
    await act(async () => tree.unmount());
  });

  test("offline and error surfaces never enable mutations or hide recovery", async () => {
    const tree = await mount({ ...base(), connected: false });
    expect(button(tree, "Disconnect")).toBeUndefined();
    expect(tree.root.findAllByType("input" as any)).toHaveLength(0);
    expect(JSON.stringify(tree.toJSON())).toContain("Server offline");
    let retries = 0;
    await act(async () => tree.update(<TelegramConnectionPanel {...base()} error="Connection unavailable" onRetry={() => retries++} />));
    await act(async () => button(tree, "Retry").props.onPress());
    expect(retries).toBe(1);
    expect(JSON.stringify(tree.toJSON())).toContain("Connection unavailable");
    await act(async () => tree.unmount());
  });

  test("reconnect and unbound states preserve explicit actions, recipient text and scroll safe area", async () => {
    const props=base();
    const tree=await mount({...props,status:{...props.status!,state:"disabled",enabled:false,topics_available:false,recipient_id:"session-a",recipient_label:"A long Session recipient whose name must wrap"}});
    expect(button(tree,"Reconnect")).toBeDefined();
    expect(JSON.stringify(tree.toJSON())).toContain("A long Session recipient");
    expect(tree.root.findByType("scroll" as any).props.contentContainerStyle[1].paddingBottom).toBeGreaterThan(34);
    await act(async()=>tree.update(<TelegramConnectionPanel {...props} status={{state:"setup_pending",enabled:true,binding_pending:true,bot_username:"fixture_bot"}} />));
    expect(button(tree,"Open connection link")).toBeDefined();
    expect(button(tree,"Disconnect")).toBeUndefined();
    await act(async()=>tree.unmount());
  });
}
