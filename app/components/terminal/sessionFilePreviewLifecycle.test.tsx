import { expect, test, mock } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import type { SessionFileMetadata } from "../../services/sessionFilePreview";

if (!process.env.ZEN_PREVIEW_HOOK_CHILD) {
  test("production preview cold-open and request-lifetime behavior", () => {
    const result = Bun.spawnSync(
      [process.execPath, "test", import.meta.filename],
      { env: { ...process.env, ZEN_PREVIEW_HOOK_CHILD: "1" } },
    );
    if (result.exitCode)
      throw new Error(
        new TextDecoder().decode(result.stdout) +
          new TextDecoder().decode(result.stderr),
      );
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const host = (name: string) => (props: any) =>
    React.createElement(name, props, props.children);
  mock.module("react-native", () => ({
    ActivityIndicator: host("spinner"),
    ScrollView: host("scroll"),
    Text: host("text"),
    TouchableOpacity: host("button"),
    View: host("view"),
    StyleSheet: { create: (value: unknown) => value, hairlineWidth: 1 },
  }));
  mock.module("@expo/vector-icons", () => ({ Ionicons: host("icon") }));
  mock.module("expo-clipboard", () => ({ setStringAsync: async () => {} }));
  mock.module("../../constants/tokens", () => ({ Typography: {} }));
  mock.module("../../services/websocket", () => ({ wsClient: {} }));
  mock.module("../../services/sessionFilePreviewDownload.expo", () => ({
    createExpoSessionFileDownloadBackend: () => ({}),
  }));
  mock.module("../ui", () => ({
    BottomSheetFrame: ({ visible, children }: any) =>
      visible ? <>{children}</> : null,
  }));
  mock.module("./InterfaceMessageBody", () => ({
    MessageBody: host("markdown"),
  }));
  mock.module("./SessionFilePdfPreview", () => ({
    SessionFilePdfPreview: host("pdf"),
  }));
  const gesture = () => ({
    minDistance() {
      return this;
    },
    onUpdate() {
      return this;
    },
    onEnd() {
      return this;
    },
  });
  mock.module("react-native-gesture-handler", () => ({
    Gesture: { Pinch: gesture, Pan: gesture, Simultaneous: () => ({}) },
    GestureDetector: ({ children }: any) => children,
  }));
  mock.module("react-native-reanimated", () => ({
    default: { Image: host("image") },
    useSharedValue: (value: unknown) => React.useRef({ value }).current,
    useAnimatedStyle: (worklet: () => unknown) => worklet(),
  }));
  const { SessionFilePreviewSheet } = await import("./SessionFilePreviewSheet");
  const metadata = (path: string): SessionFileMetadata => ({
    name: path,
    path,
    relativePath: path,
    kind: "image",
    contentType: "image/png",
    size: 200,
    modifiedAt: "",
    generation: "generation-1",
    tooLarge: false,
    previewLimitBytes: 16000000,
  });

  test("first unprepared image opens once, second open works, closing ignores a late first response", async () => {
    const requests: string[] = [];
    const binary: string[] = [];
    let resolveSlow!: (value: SessionFileMetadata) => void;
    const loader = {
      metadata: async (_server: string, request: { path: string }) => {
        requests.push(request.path);
        return request.path === "slow.png"
          ? new Promise<SessionFileMetadata>((resolve) => {
              resolveSlow = resolve;
            })
          : metadata(request.path);
      },
      binary: async (
        _server: string,
        _daemon: string,
        request: { path: string },
      ) => {
        binary.push(request.path);
        return { uri: "fixture:" + request.path, headers: {} };
      },
      text: async () => {
        throw Error("Not text");
      },
    };
    const props = {
      serverId: "fixture",
      serverUrl: "http://fixture.invalid",
      daemonId: "fixture",
      workerId: "fixture",
      processId: 1,
      startedAt: 1,
      chrome: {} as any,
      theme: {} as any,
      onClose() {},
      loader,
    };
    let renderer!: TestRenderer.ReactTestRenderer;
    await act(async () => {
      renderer = TestRenderer.create(
        <SessionFilePreviewSheet {...props} reference={null} />,
      );
    });
    expect(requests).toEqual([]);
    const open = (reference: string | null) =>
      act(async () =>
        renderer.update(
          <SessionFilePreviewSheet {...props} reference={reference} />,
        ),
      );
    await open("normal.png");
    expect(requests).toEqual(["normal.png"]);
    expect(binary).toEqual(["normal.png"]);
    expect(renderer.root.findByType("image").props.source.uri).toBe(
      "fixture:normal.png",
    );
    await open(null);
    expect(renderer.root.findAllByType("image")).toHaveLength(0);
    await open("normal.png");
    expect(binary).toHaveLength(2);
    await open("slow.png");
    expect(renderer.root.findAllByType("image")).toHaveLength(0);
    await open(null);
    await open("tall.png");
    await act(async () => resolveSlow(metadata("slow.png")));
    expect(renderer.root.findByType("image").props.source.uri).toBe(
      "fixture:tall.png",
    );
    expect(binary).not.toContain("slow.png");
    await act(async () => renderer.unmount());
  });
}
