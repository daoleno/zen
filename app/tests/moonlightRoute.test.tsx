import { expect, mock, test } from "bun:test";
import { createRequire } from "node:module";

/**
 * Mounted production route regression for the Review1127 reproductions,
 * inverted: absolute pointer input, scroll, ordinary Stop disconnects instead
 * of revoking, and stale callbacks from an unmounted View cannot clear the new
 * connection. Native IO is mocked; no simulator or graphics are involved.
 */
const root = new URL("..", import.meta.url).pathname.replace(/\/$/, "");
const require = createRequire(root + "/package.json");
const React = require("react");
const { act, create } = require("react-test-renderer");
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const host = (name: string) => (props: any) => React.createElement(name, props, props.children);
const calls: { name: string; args: unknown[] }[] = [];
let responder: any;
const currentServer = { id: "owned", name: "Owned", url: "ws://192.0.2.10:9876/ws", daemonId: "identity", daemonPublicKey: "key" };

mock.module(require.resolve("react-native"), () => ({
  AppState: { addEventListener: () => ({ remove() {} }) },
  Keyboard: { addListener: () => ({ remove() {} }), dismiss() {} },
  KeyboardAvoidingView: host("avoid"),
  PanResponder: { create: (value: unknown) => { responder = value; return { panHandlers: {} }; } },
  Platform: { OS: "android" }, Pressable: host("button"), Text: host("text"),
  TextInput: host("input"), View: host("view"),
  StyleSheet: { create: (value: unknown) => value, hairlineWidth: 1, absoluteFill: {} },
  useWindowDimensions: () => ({ width: 1000, height: 1000, scale: 1, fontScale: 1 }),
}));
mock.module(require.resolve("@expo/vector-icons"), () => ({ Ionicons: host("icon"), MaterialIcons: host("icon") }));
mock.module(require.resolve("expo-router"), () => ({
  Stack: { Screen: host("screen") }, router: { push() {} },
  useFocusEffect: (effect: any) => React.useEffect(effect, [effect]),
}));
mock.module(require.resolve("react-native-safe-area-context"), () => ({ SafeAreaView: host("safe") }));
mock.module(require.resolve(root + "/store/currentServer"), () => ({ useCurrentServer: () => ({ currentServer, isCurrentServer: () => true }) }));
mock.module(require.resolve(root + "/constants/tokens"), () => ({ useAppColors: () => ({}) }));
mock.module(root + "/services/remoteDesktop.ts", () => ({
  prepareDesktopConnection: async () => JSON.stringify({ transport: "moonlight", moonlight: {
    host: "192.0.2.10", hostKey: "host-key", identityKey: "owned-device", httpPort: 47989, httpsPort: 47984,
  } }),
}));
mock.module(root + "/services/desktopScopeGrant.ts", () => ({ enableDesktopScope: async () => {} }));
mock.module(root + "/services/confirmDesktopEnable.ts", () => ({ confirmDesktopEnable: async () => true }));
mock.module(root + "/modules/zen-remote-desktop/src/index.ts", () => ({
  NativeDesktopKeyboard: null,
  NativeDesktopView: host("old-native"),
  NativeMoonlightDesktopView: React.forwardRef((props: any, ref: any) => {
    React.useImperativeHandle(ref, () => Object.fromEntries(
      ["disconnect", "revoke", "sendPointerMove", "sendPointerPosition", "sendPointerButton", "sendScroll", "sendKey", "sendText"]
        .map((name) => [name, async (...args: unknown[]) => { calls.push({ name, args }); return true; }]),
    ));
    return React.createElement("moonlight-native", props);
  }),
}));
const { default: Screen } = await import(root + "/app/remote-desktop.tsx");

test("mounted route sends absolute input, disconnect on Stop and ignores stale callbacks", async () => {
  let tree: any;
  const clickConnect = async () => act(async () => {
    tree.root.findAllByType("button").find((button: any) =>
      button.findAllByType("text").some((text: any) => text.children.includes("Connect")))!.props.onPress();
  });
  const clickTool = async (label: string) => act(async () => {
    tree.root.findAllByType("button").find((button: any) => button.props.accessibilityLabel === label).props.onPress();
  });
  const connected = { nativeEvent: { state: "connected", generation: 1, width: 1000, height: 1000 } };
  try {
    await act(async () => { tree = create(React.createElement(Screen)); });
    await clickConnect();
    const oldCallback = tree.root.findByType("moonlight-native").props.onState;
    await act(async () => oldCallback(connected));
    await act(async () => tree.root.findAllByType("view").find((view: any) => view.props.onLayout)
      .props.onLayout({ nativeEvent: { layout: { x: 0, y: 0, width: 1000, height: 1000 } } }));
    await act(async () => {
      for (let x = 100; x <= 900; x += 100) {
        responder.onPanResponderMove({ nativeEvent: { locationX: x, locationY: 100, touches: [{}] } }, {});
      }
    });
    const positions = calls.filter((call) => call.name === "sendPointerPosition");
    expect(positions.length).toBe(9);
    expect(positions[0].args[1]).toBe(100);
    expect(positions[8].args[1]).toBe(900);
    expect(positions[8].args[3]).toBe(1000);
    expect(calls.filter((call) => call.name === "sendPointerMove")).toHaveLength(0);

    await clickTool("Scroll up");
    expect(calls.some((call) => call.name === "sendScroll" && call.args[1] === -3)).toBe(true);

    await clickTool("Stop desktop");
    expect(calls.some((call) => call.name === "disconnect")).toBe(true);
    expect(calls.some((call) => call.name === "revoke")).toBe(false);

    await clickConnect();
    const newCallback = tree.root.findByType("moonlight-native").props.onState;
    await act(async () => newCallback(connected));
    expect(tree.root.findAllByType("moonlight-native")).toHaveLength(1);
    await act(async () => oldCallback({ nativeEvent: { state: "disconnected", generation: 1, reason: "late old connection" } }));
    expect(tree.root.findAllByType("moonlight-native")).toHaveLength(1);
  } finally {
    if (tree) await act(async () => tree.unmount());
  }
});
