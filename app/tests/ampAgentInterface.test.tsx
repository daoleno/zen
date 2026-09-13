import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

if (!process.env.ZEN_AMP_INTERFACE_CHILD) {
  test("mounted Amp peer selection boundaries", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, ZEN_AMP_INTERFACE_CHILD: "1" }, timeout: 20_000,
    });
    if (result.exitCode) throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const host = (name: string) => (props: any) => React.createElement(name, props, props.children);
  let currentServer = "server-a";
  mock.module("react-native", () => ({
    ActivityIndicator: host("busy"), ScrollView: host("scroll"), Text: host("text"), View: host("view"),
    TouchableOpacity: host("button"), Pressable: host("button"), Keyboard: { dismiss() {} },
    FlatList: (props: any) => <>{props.data.map((item: any) => <React.Fragment key={item.id}>{props.renderItem({ item })}</React.Fragment>)}</>,
    StyleSheet: { create: (value: unknown) => value, hairlineWidth: 1 },
  }));
  mock.module("@expo/vector-icons", () => ({ Ionicons: host("icon") }));
  mock.module("expo-haptics", () => ({ ImpactFeedbackStyle: { Light: "light" }, impactAsync() {}, selectionAsync() {} }));
  mock.module("../constants/tokens", () => ({
    useAppColors: () => ({}), useAppTheme: () => ({ theme: { colors: {} } }),
    UiTextMetrics: {}, Radii: { md: 16, pill: 999 }, Typography: {},
    TypeScale: { title: {}, compact: {}, caption: {}, label: {}, micro: {} }, uiLineHeight: (size: number) => size * 1.5,
  }));
  mock.module("../constants/themedSurfaces", () => ({ surfacesFromTheme: () => ({}) }));
  mock.module("../components/ui/AnimatedPressable", () => ({ AnimatedPressable: host("button") }));
  mock.module("../components/ui/BottomSheetFrame", () => ({ BottomSheetFrame: host("sheet") }));
  mock.module("../components/ui", () => ({ AppText: host("text"), BottomSheetFrame: host("sheet") }));
  mock.module("../components/terminal/AgentKindIcon", () => ({ AgentKindIcon: host("agent-icon") }));
  mock.module("../components/brain/BrainExecutorIcon", () => ({ BrainExecutorIcon: host("agent-icon") }));
  mock.module("../store/currentServer", () => ({ useCurrentServer: () => ({ isCurrentServer: (id: string) => id === currentServer }) }));
  mock.module("../services/websocket", () => ({ wsClient: { listDir: () => { throw new Error("unexpected directory request"); } } }));
  mock.module("../components/terminal/DirectoryPickerContent", () => ({ DirectoryPickerContent: host("directory") }));
  mock.module("../components/terminal/NewTerminalSheetContent", () => ({ NewTerminalSheetContent: host("form") }));

  const { NewTerminalLaunchPresetList } = await import("../components/terminal/NewTerminalLaunchPresetList");
  const { NewTerminalSheet } = await import("../components/terminal/NewTerminalSheet");
  const { BrainExecutorSheet } = await import("../components/brain/BrainExecutorSheet");
  const { BrainExecutorMentionPicker } = await import("../components/brain/BrainExecutorMentionPicker");
  const { AMP_COMMAND } = await import("../services/agentCommands");
  const { AMP_LAUNCH_LIMITATION, AMP_DELEGATION_LIMITATION } = await import("../services/ampAgent");
  const buttons = (tree: TestRenderer.ReactTestRenderer) => tree.root.findAllByType("button" as any);
  const ampButton = (tree: TestRenderer.ReactTestRenderer) => buttons(tree).find((node) => node.props.accessibilityLabel?.startsWith("Amp."))!;
  const mount = async (element: React.ReactElement) => {
    let tree!: TestRenderer.ReactTestRenderer;
    await act(async () => { tree = TestRenderer.create(element); });
    return tree;
  };

  test("Session Amp is disabled with visible status; Pi and OpenCode retain commands", async () => {
    const selected: string[] = [];
    const tree = await mount(<NewTerminalLaunchPresetList command="" submitting={false} canSubmit onPresetPress={(preset) => selected.push(preset.command)} />);
    const amp = ampButton(tree);
    expect(amp.props.disabled).toBe(true);
    expect(amp.props.accessibilityState).toEqual({ disabled: true, selected: false });
    await act(async () => amp.props.onPress());
    expect(selected).toEqual([]);
    expect(JSON.stringify(tree.toJSON())).toContain(AMP_LAUNCH_LIMITATION);
    expect(JSON.stringify(tree.toJSON())).toContain("CLI and Amp account: not verified");
    for (const label of ["Pi", "OpenCode"]) {
      await act(async () => buttons(tree).find((node) => node.props.accessibilityLabel === label)!.props.onPress());
    }
    expect(selected).toEqual(["pi", "opencode"]);
    await act(async () => tree.update(<NewTerminalLaunchPresetList command="" submitting={false} canSubmit={false} onPresetPress={(preset) => selected.push(preset.command)} />));
    await act(async () => buttons(tree).find((node) => node.props.accessibilityLabel === "Codex")!.props.onPress());
    expect(selected).toEqual(["pi", "opencode"]);
    await act(async () => tree.unmount());
  });

  test("Session owner rejects disabled preset and direct Amp advanced submit before creating a Session", async () => {
    const launched: unknown[] = [];
    const tree = await mount(<NewTerminalSheet visible title="Session" serverId="server-a" initialCommand={AMP_COMMAND} initialCwd="/repo" onClose={() => {}} onSubmit={(input) => launched.push(input)} />);
    const form = () => tree.root.findByType("form" as any);
    await act(async () => form().props.onPresetPress({ key: "amp", kind: "amp", label: "Amp", command: AMP_COMMAND, unavailableReason: AMP_LAUNCH_LIMITATION }));
    await act(async () => form().props.onSubmitAdvanced());
    expect(launched).toEqual([]);
    await act(async () => form().props.onPresetPress({ key: "pi", kind: "pi", label: "Pi", command: "pi" }));
    expect(launched).toEqual([{ cwd: "/repo", command: "pi", name: "", serverId: "server-a" }]);
    currentServer = "server-b";
    await act(async () => form().props.onPresetPress({ key: "pi", kind: "pi", label: "Pi", command: "pi" }));
    expect(launched).toHaveLength(1);
    await act(async () => tree.unmount());
    currentServer = "server-a";
  });

  test("Brain and Workers never dispatch Amp selection, even when it exists in the catalog", async () => {
    const selected: string[] = [];
    const peers = ["codex", "pi", "grok"].map((id) => ({ id, name: id, provider: id }));
    const props = { visible: true, executors: peers, switchingAdapterId: null, switchingTarget: null, onClose() {}, onSelect: (adapter: { id: string }, target: string) => selected.push(`${target}:${adapter.id}`) };
    const tree = await mount(<BrainExecutorSheet {...props} />);
    expect(ampButton(tree).props.disabled).toBe(true);
    await act(async () => ampButton(tree).props.onPress());
    expect(JSON.stringify(tree.toJSON())).toContain(AMP_DELEGATION_LIMITATION);
    await act(async () => buttons(tree).find((node) => node.props.accessibilityLabel === "Workers executor")!.props.onPress());
    await act(async () => ampButton(tree).props.onPress());
    await act(async () => buttons(tree).find((node) => node.props.accessibilityLabel === "Set Worker executor to pi")!.props.onPress());
    expect(selected).toEqual(["workers:pi"]);
    await act(async () => tree.update(<BrainExecutorSheet {...props} executors={[...peers, { id: "amp", name: "amp", provider: "custom", command: AMP_COMMAND }]} />));
    expect(buttons(tree).filter((node) => node.props.accessibilityLabel?.startsWith("Amp."))).toHaveLength(1);
    await act(async () => ampButton(tree).props.onPress());
    expect(selected).toEqual(["workers:pi"]);
    await act(async () => tree.unmount());
  });

  test("mention search presents Amp limitation without inserting a delegating mention", async () => {
    const selected: string[] = [];
    const tree = await mount(<BrainExecutorMentionPicker executors={[{ id: "pi", name: "Pi" }]} query="am" chrome={{} as any} onSelect={(adapter) => selected.push(adapter.id)} />);
    expect(ampButton(tree).props.disabled).toBe(true);
    await act(async () => ampButton(tree).props.onPress());
    expect(selected).toEqual([]);
    expect(JSON.stringify(tree.toJSON())).toContain(AMP_DELEGATION_LIMITATION);
    await act(async () => tree.unmount());
  });
}
