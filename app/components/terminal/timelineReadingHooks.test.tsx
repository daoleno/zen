import { describe, expect, test, mock } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import type { NativeScrollEvent, NativeSyntheticEvent } from "react-native";

// Keep native host mocks out of the shared Bun process; execute real React hooks.
if (!process.env.ZEN_READING_HOOK_CHILD) {
  test("production reading hooks behavioral scenarios", () => {
    const result = Bun.spawnSync(
      [process.execPath, "test", import.meta.filename],
      { env: { ...process.env, ZEN_READING_HOOK_CHILD: "1" } },
    );
    if (result.exitCode !== 0)
      throw new Error(
        new TextDecoder().decode(result.stdout) +
          new TextDecoder().decode(result.stderr),
      );
    expect(
      new TextDecoder().decode(result.stdout) +
        new TextDecoder().decode(result.stderr),
    ).not.toContain("(fail)");
    expect(result.exitCode).toBe(0);
  });
} else {
  mock.module("react-native", () => ({
    Platform: { OS: "android" },
    Keyboard: { dismiss() {} },
  }));
  mock.module("react-native-reanimated", () => ({
    useReducedMotion: () => true,
    useSharedValue: (value: unknown) => React.useRef({ value }).current,
  }));
  mock.module("./InterfaceChatSurfaceModel", () => ({
    buildInterfaceComposerPresentation: () => ({}),
  }));
  mock.module("../../services/chatComposerPresentation", () => ({
    agentKindFromCommand: () => "codex",
  }));
  const { usePinnedTimeline } = await import("./InterfaceChatSurfaceHooks");
  const frames = new Map<number, FrameRequestCallback>();
  let frameId = 0;
  globalThis.requestAnimationFrame = (callback) => {
    frames.set(++frameId, callback);
    return frameId;
  };
  globalThis.cancelAnimationFrame = (id) => {
    if (id != null) frames.delete(id);
  };
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const flush = () => {
    const callbacks = [...frames.values()];
    frames.clear();
    callbacks.forEach((callback) => callback(0));
  };
  const scroll = (
    y: number,
    height = 2000,
  ): NativeSyntheticEvent<NativeScrollEvent> =>
    ({
      nativeEvent: {
        contentOffset: { y, x: 0 },
        contentInset: { top: 0 },
        contentSize: { height },
        layoutMeasurement: { height: 500 },
        velocity: { y: 0 },
      },
    }) as NativeSyntheticEvent<NativeScrollEvent>;

  function harness(scope: string, count = 20) {
    let hook!: ReturnType<typeof usePinnedTimeline>;
    const calls: { offset: number; animated: boolean }[] = [];
    function Host({ count }: { count: number }) {
      hook = usePinnedTimeline(count, scope);
      hook.scrollRef.current = {
        scrollToOffset: (value: { offset: number; animated: boolean }) =>
          calls.push(value),
      } as any;
      return null;
    }
    let renderer!: TestRenderer.ReactTestRenderer;
    act(() => {
      renderer = TestRenderer.create(<Host count={count} />);
    });
    const ids = Array.from({ length: count }, (_, i) => `row-${i}`);
    const layout = (shift = 0) =>
      act(() => {
        hook.readingPosition.onItems(ids);
        ids.forEach((id, i) =>
          hook.readingPosition.onCellLayout(id, {
            offset: i * 100 + shift,
            length: 100,
          }),
        );
        hook.handleLayout({ nativeEvent: { layout: { height: 500 } } } as any);
        hook.handleTurnFocusSpacerLayout(
          hook.turnFocusSpacer.value.height,
          hook.turnFocusSpacer.value.requestEpoch,
        );
        hook.handleContentSizeChange(360, 2000 + shift);
        flush();
      });
    return {
      get hook() {
        return hook;
      },
      calls,
      ids,
      layout,
      update: (count: number) =>
        act(() => renderer.update(<Host count={count} />)),
      close: () => act(() => renderer.unmount()),
    };
  }

  describe("production reading ownership", () => {
    test("provider echo identity preserves the existing reading anchor rather than selecting a neighbor", () => {
      const h = harness("hook-provider-echo");
      h.layout();
      act(() => {
        h.hook.handleScrollBeginDrag();
        h.hook.handleScrollEndDrag(scroll(450));
      });
      act(() => {
        h.hook.readingPosition.onItems(h.ids.map(id => id === "row-9" ? "echo-9" : id), "echo-9");
        h.hook.readingPosition.onCellLayout("echo-9", { offset: 1100, length: 100 });
        flush();
      });
      expect(h.hook.readingPosition.current().anchor).toMatchObject({ id: "echo-9", intraOffset: 50 });
      expect(h.calls.at(-1)?.offset).toBe(650);
      h.close();
    });
    test("a shortened viewport extent cannot cause repeated unreachable restoration commands", () => {
      const h = harness("hook-clamped-anchor");
      h.layout();
      act(() => {
        h.hook.handleScrollBeginDrag();
        h.hook.handleScrollEndDrag(scroll(1450));
      });
      h.calls.length = 0;
      act(() => {
        h.hook.handleContentSizeChange(360, 1000);
        flush();
      });
      expect(h.calls.at(-1)?.offset).toBe(500);
      const count = h.calls.length;
      act(() => {
        h.hook.handleScroll(scroll(500, 1000));
        flush();
      });
      expect(h.calls).toHaveLength(count);
      expect(h.hook.readingPosition.current().mode).toBe("detached");
      h.close();
    });
    test("history survives append, streaming height and native momentum compensation", () => {
      const h = harness("hook-history");
      h.layout();
      h.calls.length = 0;
      act(() => {
        h.hook.handleScrollBeginDrag();
        h.hook.handleScrollEndDrag(scroll(450));
      });
      const before = h.hook.readingPosition.current();
      expect(before.mode).toBe("detached");
      expect(before.anchor?.id).toBe("row-9");
      expect(before.anchor?.intraOffset).toBe(50);
      h.update(21);
      h.layout(200);
      expect(h.calls.at(-1)?.offset).toBe(650);
      expect(h.hook.readingPosition.current().anchor).toEqual(before.anchor);
      act(() => {
        h.hook.handleScroll(scroll(650, 2200));
        h.hook.handleScrollBeginDrag();
        h.hook.handleMomentumScrollBegin();
      });
      h.calls.length = 0;
      h.layout(400);
      expect(h.calls).toHaveLength(0);
      act(() => h.hook.handleScroll(scroll(850, 2400)));
      expect(h.hook.readingPosition.current().mode).toBe("detached");
      h.close();
    });

    test("same-conversation remount restores measured message and intra-offset; another server does not inherit it", () => {
      const h = harness("server-a:conversation-restore");
      h.layout();
      act(() => {
        h.hook.handleScrollBeginDrag();
        h.hook.handleScrollEndDrag(scroll(450));
      });
      h.close();
      const other = harness("server-b:conversation-restore");
      expect(other.hook.readingPosition.current().mode).toBe("attached");
      other.close();
      const restored = harness("server-a:conversation-restore");
      expect(restored.hook.readingPosition.initialAnchor?.id).toBe("row-9");
      restored.layout(300);
      expect(restored.calls.at(-1)?.offset).toBe(750);
      expect(restored.hook.readingPosition.current().anchor?.intraOffset).toBe(
        50,
      );
      restored.close();
    });

    test("Android translated content origin is included once across composer and navigation layout", () => {
      const h = harness("hook-native-inset");
      h.layout();
      act(() => {
        h.hook.readingPosition.onInsetChange(64);
        h.hook.handleScrollBeginDrag();
        h.hook.handleScrollEndDrag(scroll(514));
      });
      expect(h.hook.readingPosition.current().anchor?.intraOffset).toBe(50);
      h.close();
      const restored = harness("hook-native-inset");
      restored.layout();
      expect(restored.calls.at(-1)?.offset).toBe(450);
      act(() => {
        restored.hook.readingPosition.onInsetChange(64);
        flush();
      });
      expect(restored.calls.at(-1)?.offset).toBe(514);
      expect(restored.hook.readingPosition.current().observedIntraOffset).toBe(
        50,
      );
      restored.close();
    });

    test("native screen measurement wins over layout estimates and stale unmount callbacks cannot replace the bookmark", () => {
      const h = harness("hook-native-measure");
      h.layout();
      act(() => {
        h.hook.handleScrollBeginDrag();
        h.hook.handleScrollEndDrag(scroll(450));
      });
      h.calls.length = 0;
      act(() => {
        h.hook.readingPosition.onViewportOrigin(40);
        h.hook.readingPosition.onCellLayout(
          "row-9",
          { offset: 1100, length: 100 },
          (callback) => callback(12, 100),
        );
        flush();
      });
      expect(h.calls.at(-1)?.offset).toBe(428);
      let late: ((y: number, height: number) => void) | undefined;
      act(() => {
        h.hook.readingPosition.onCellLayout(
          "row-9",
          { offset: 1100, length: 100 },
          (callback) => {
            late = callback;
          },
        );
        h.hook.handleScrollBeginDrag();
        h.hook.handleScrollEndDrag(scroll(450));
      });
      const saved = h.hook.readingPosition.current().anchor;
      h.close();
      late?.(0, 0);
      const restored = harness("hook-native-measure");
      expect(restored.hook.readingPosition.initialAnchor).toEqual(saved);
      restored.close();
    });

    test("keyboard decorator compensation is not undone by a second JS scroll", () => {
      const h = harness("hook-inset-compensation");
      h.layout();
      let nativeY = -460;
      act(() => {
        h.hook.readingPosition.onViewportOrigin(40);
        h.hook.readingPosition.onCellLayout(
          "row-9",
          { offset: 900, length: 100 },
          (callback) => callback(nativeY, 100),
        );
        nativeY = -10;
        h.hook.handleScrollBeginDrag();
        h.hook.handleScrollEndDrag(scroll(450));
      });
      expect(h.hook.readingPosition.current().anchor?.nativeInset).toBe(0);
      h.calls.length = 0;
      act(() => {
        nativeY = 54;
        h.hook.handleScroll(scroll(514));
        h.hook.readingPosition.onInsetChange(64);
        flush();
      });
      expect(h.calls).toHaveLength(0);
      expect(h.hook.readingPosition.current().observedIntraOffset).toBe(50);
      h.close();
    });

    test("empty reconnect and layout never change detached intent; explicit latest resumes follow", () => {
      const h = harness("hook-reconnect");
      h.layout();
      act(() => {
        h.hook.handleScrollBeginDrag();
        h.hook.handleScrollEndDrag(scroll(450));
      });
      h.update(0);
      expect(h.hook.readingPosition.current().mode).toBe("detached");
      h.update(20);
      h.layout();
      expect(h.hook.readingPosition.current().mode).toBe("detached");
      act(() => h.hook.scrollToLatest(false));
      expect(h.hook.readingPosition.current().mode).toBe("attached");
      expect(h.calls.at(-1)?.offset).toBe(0);
      h.layout(200);
      expect(h.calls.at(-1)?.offset).toBe(0);
      h.close();
    });

    test("manual history movement cancels delayed send focus and selection owns its viewport", () => {
      const h = harness("hook-focus");
      h.layout();
      act(() => {
        h.hook.requestTurnFocus("pending-send");
        h.hook.handleScrollBeginDrag();
        h.hook.handleScrollEndDrag(scroll(450));
      });
      h.calls.length = 0;
      act(() => {
        h.hook.handleTurnFocusAnchorAvailable("pending-send");
        h.hook.handleTurnFocusRowLayout("pending-send", 100, 0);
        h.hook.handleTurnFocusSpacerLayout(500, 1);
        flush();
      });
      expect(h.calls).toHaveLength(0);
      expect(h.hook.readingPosition.current().mode).toBe("detached");
      act(() => h.hook.handleTextSelectionGestureStart());
      h.layout(200);
      expect(h.calls).toHaveLength(0);
      h.close();
    });
  });
}
