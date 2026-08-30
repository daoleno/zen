import React, {
  useCallback,
  useEffect,
  useLayoutEffect,
  useId,
  useRef,
  useState,
} from "react";
import { AppState, Platform, StyleSheet, View } from "react-native";
import { LinearGradient } from "expo-linear-gradient";
import { useGenericKeyboardHandler } from "react-native-keyboard-controller";
import { runOnUIAsync } from "react-native-worklets";
import Reanimated, {
  runOnJS,
  useDerivedValue,
  useAnimatedStyle,
  useSharedValue,
  type SharedValue,
} from "react-native-reanimated";
import { getZenKeyboardForegroundSnapshot } from "zen-keyboard-lifecycle";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { withAlpha } from "./colorWithAlpha";
import {
  reapplyStructuredChatWindowModeLease,
  useStructuredChatWindowMode,
} from "./chatKeyboardWindowMode";
import { useInterfaceComposerLayout } from "./useInterfaceComposerLayout";
import { StructuredChatContentFade } from "./StructuredChatContentFade";
import {
  createStructuredChatKeyboardLifecycleGate,
  dispatchStructuredChatAuthoritativeSnapshot,
  dispatchStructuredChatKeyboardLifecycleEvent,
  reduceStructuredChatKeyboardLifecycleGate,
  resolveInterfaceChatCanvasColor,
  structuredChatGatedOverlayTranslateY,
  structuredChatScrollClearance,
  type StructuredChatKeyboardLifecycleDispatchResult,
  type StructuredChatKeyboardLifecycleGate,
} from "./chatKeyboardOverlayPolicy";

interface InterfaceChatKeyboardFrameProps {
  enabled: boolean;
  composerFocused?: boolean;
  keyboardVerticalOffset: number;
  chrome: TerminalThemeChrome;
  topChromeInset?: number;
  renderTimeline(
    extraContentPadding: SharedValue<number>,
    keyboardLifecycleGate: SharedValue<StructuredChatKeyboardLifecycleGate>,
  ): React.ReactNode;
  onKeyboardLifecycleInvalidate?: (
    reason: "route" | "app" | "foreground_closed" | "ime_closed",
  ) => void;
  composer?: React.ReactNode;
  floatingAction?: React.ReactNode;
  portal?: React.ReactNode;
}

// Mirrors KeyboardProvider's platform cadence: iOS publishes destination
// geometry on start, Android publishes live geometry on move/end, and both
// publish interactive samples.
const KEYBOARD_GEOMETRY_PLATFORM = Platform.OS;

export function InterfaceChatKeyboardFrame({
  enabled,
  composerFocused = false,
  keyboardVerticalOffset,
  chrome,
  topChromeInset = 0,
  renderTimeline,
  onKeyboardLifecycleInvalidate,
  composer,
  floatingAction,
  portal,
}: InterfaceChatKeyboardFrameProps) {
  const overlayEnabled = Boolean(composer);
  const { composerHeight, handleComposerLayout } = useInterfaceComposerLayout({
    enabled: overlayEnabled,
  });
  const reactId = useId();
  const composerNativeId = `zen-structured-chat-composer-${reactId}`;
  const initialKeyboardLifecycleGate =
    createStructuredChatKeyboardLifecycleGate({
      enabled,
      appActive: AppState.currentState === "active",
    });
  // KeyboardController provides raw native samples; this shared value is the
  // sole accepted lifecycle + geometry owner for both layout consumers.
  const keyboardLifecycleGate = useSharedValue(initialKeyboardLifecycleGate);
  // A handler closure captures the revision in which it was registered. Native
  // callbacks already queued by the previous Activity/focus lifetime can then
  // be rejected instead of being credited to whichever revision is current when
  // they finally arrive.
  const [nativeCallbackRevision, setNativeCallbackRevision] = useState(
    initialKeyboardLifecycleGate.revision,
  );
  const appStateRef = useRef(AppState.currentState);
  useStructuredChatWindowMode(enabled);
  const disposedRef = useRef(false);
  useEffect(() => {
    disposedRef.current = false;
    return () => {
      disposedRef.current = true;
    };
  }, []);
  const handleGateDispatch = useCallback(
    (result: StructuredChatKeyboardLifecycleDispatchResult) => {
      if (disposedRef.current) {
        return;
      }
      setNativeCallbackRevision(result.revision);
      if (result.invalidateReason) {
        onKeyboardLifecycleInvalidate?.(result.invalidateReason);
      }
    },
    [onKeyboardLifecycleInvalidate],
  );
  const nativeSnapshotRequestsRef = useRef(new Set<string>());
  const requestAuthoritativeSnapshot = useCallback(
    (sourceRevision: number, foregroundReconciliation: boolean) => {
      const requestKey = `${sourceRevision}:${foregroundReconciliation ? 1 : 0}`;
      if (nativeSnapshotRequestsRef.current.has(requestKey)) {
        return;
      }
      nativeSnapshotRequestsRef.current.add(requestKey);
      void getZenKeyboardForegroundSnapshot(composerNativeId, sourceRevision)
        .catch(() => ({
          revision: sourceRevision,
          imeVisible: false,
          imeHeight: 0,
          composerFocused: false,
          evidence: "snapshot_failed",
        }))
        .then((snapshot) =>
          runOnUIAsync(
            dispatchStructuredChatAuthoritativeSnapshot,
            keyboardLifecycleGate,
            {
              type: "authoritative_snapshot" as const,
              sourceRevision: snapshot.revision,
              imeVisible: snapshot.imeVisible,
              imeHeight: snapshot.imeHeight,
              composerFocused: snapshot.composerFocused,
              foregroundReconciliation,
            },
          ),
        )
        .then((result) => {
          if (!disposedRef.current && result.shouldBlurComposer) {
            onKeyboardLifecycleInvalidate?.(
              foregroundReconciliation ? "foreground_closed" : "ime_closed",
            );
          }
        })
        .finally(() => {
          nativeSnapshotRequestsRef.current.delete(requestKey);
        });
    },
    [composerNativeId, keyboardLifecycleGate, onKeyboardLifecycleInvalidate],
  );

  useLayoutEffect(() => {
    void runOnUIAsync(
      dispatchStructuredChatKeyboardLifecycleEvent,
      keyboardLifecycleGate,
      { type: "set_enabled", enabled },
    ).then((result) => {
      handleGateDispatch(result);
      if (enabled && AppState.currentState === "active") {
        requestAuthoritativeSnapshot(result.revision, false);
      }
    });
  }, [
    enabled,
    keyboardLifecycleGate,
    handleGateDispatch,
    requestAuthoritativeSnapshot,
  ]);

  useEffect(() => {
    const updateAppState = (
      active: boolean,
      foregroundReconciliation: boolean,
    ) => {
      void runOnUIAsync(
        dispatchStructuredChatKeyboardLifecycleEvent,
        keyboardLifecycleGate,
        { type: "app_state", active },
      ).then((result) => {
        handleGateDispatch(result);
        if (active) {
          reapplyStructuredChatWindowModeLease();
          requestAuthoritativeSnapshot(
            result.revision,
            foregroundReconciliation,
          );
        }
      });
    };
    updateAppState(AppState.currentState === "active", false);
    const subscription = AppState.addEventListener("change", (state) => {
      const foregroundReconciliation =
        appStateRef.current !== "active" && state === "active";
      appStateRef.current = state;
      updateAppState(state === "active", foregroundReconciliation);
    });
    return () => subscription.remove();
  }, [keyboardLifecycleGate, handleGateDispatch, requestAuthoritativeSnapshot]);

  // React focus confirms the matching native target but cannot admit keyboard
  // geometry. The current native snapshot remains the sole OPEN authority.
  useLayoutEffect(() => {
    void runOnUIAsync(
      dispatchStructuredChatKeyboardLifecycleEvent,
      keyboardLifecycleGate,
      { type: "composer_focus", focused: enabled && composerFocused },
    ).then(handleGateDispatch);
    if (enabled && composerFocused) {
      requestAuthoritativeSnapshot(nativeCallbackRevision, false);
    }
  }, [
    composerFocused,
    enabled,
    keyboardLifecycleGate,
    handleGateDispatch,
    nativeCallbackRevision,
    requestAuthoritativeSnapshot,
  ]);

  useGenericKeyboardHandler(
    {
      onStart: (event) => {
        "worklet";
        acceptNativeKeyboardSample(
          keyboardLifecycleGate,
          nativeCallbackRevision,
          event.height,
          event.progress,
          KEYBOARD_GEOMETRY_PLATFORM === "ios",
        );
        if (
          keyboardLifecycleGate.value.authoritativeRevision !==
            keyboardLifecycleGate.value.revision ||
          !keyboardLifecycleGate.value.nativeImeVisible ||
          !keyboardLifecycleGate.value.nativeComposerFocused
        ) {
          runOnJS(requestAuthoritativeSnapshot)(nativeCallbackRevision, false);
        }
      },
      onMove: (event) => {
        "worklet";
        acceptNativeKeyboardSample(
          keyboardLifecycleGate,
          nativeCallbackRevision,
          event.height,
          event.progress,
          KEYBOARD_GEOMETRY_PLATFORM === "android",
        );
        if (
          keyboardLifecycleGate.value.authoritativeRevision !==
            keyboardLifecycleGate.value.revision ||
          !keyboardLifecycleGate.value.nativeImeVisible ||
          !keyboardLifecycleGate.value.nativeComposerFocused
        ) {
          runOnJS(requestAuthoritativeSnapshot)(nativeCallbackRevision, false);
        }
      },
      onInteractive: (event) => {
        "worklet";
        acceptNativeKeyboardSample(
          keyboardLifecycleGate,
          nativeCallbackRevision,
          event.height,
          event.progress,
          true,
        );
      },
      onEnd: (event) => {
        "worklet";
        acceptNativeKeyboardSample(
          keyboardLifecycleGate,
          nativeCallbackRevision,
          event.height,
          event.progress,
          KEYBOARD_GEOMETRY_PLATFORM === "android",
          true,
        );
        runOnJS(requestAuthoritativeSnapshot)(nativeCallbackRevision, false);
      },
    },
    [
      keyboardLifecycleGate,
      nativeCallbackRevision,
      requestAuthoritativeSnapshot,
    ],
  );

  const scrollClearance = useDerivedValue(() =>
    structuredChatScrollClearance(
      composerHeight.value,
      structuredChatGatedOverlayTranslateY({
        gate: keyboardLifecycleGate.value,
        keyboardVerticalOffset,
      }),
    ),
  );
  // The overlay transform reads the gate shared value directly instead of an
  // intermediate numeric derived value: an invalidation that lands while the
  // app is backgrounded can lose its UI->Fabric style apply, and a later
  // same-number write (0 === 0) is deduplicated so the style never re-applies.
  // Reading the gate re-triggers the style mapper on every event (each reduce
  // replaces the gate object), so a resume invalidation re-applies the
  // collapsed translation after the suspension.
  const stickyStyle = useAnimatedStyle(() => {
    return {
      transform: [
        {
          translateY: structuredChatGatedOverlayTranslateY({
            gate: keyboardLifecycleGate.value,
            keyboardVerticalOffset,
          }),
        },
      ],
    };
  }, [keyboardLifecycleGate, keyboardVerticalOffset]);
  const canvasColor = resolveInterfaceChatCanvasColor(
    chrome.appBackground,
    chrome.surface,
  );
  const transparentCanvas = withAlpha(canvasColor, 0);

  return (
    <View
      collapsable={false}
      testID="structured-chat-canvas"
      style={styles.chatCanvas}
    >
      <View collapsable={false} style={styles.timelineCanvas}>
        <StructuredChatContentFade
          canvasColor={canvasColor}
          composerHeight={composerHeight}
          keyboardLifecycleGate={keyboardLifecycleGate}
          keyboardVerticalOffset={keyboardVerticalOffset}
        >
          {renderTimeline(scrollClearance, keyboardLifecycleGate)}
        </StructuredChatContentFade>
      </View>

      <LinearGradient
        pointerEvents="none"
        colors={[canvasColor, canvasColor, transparentCanvas]}
        locations={[0, 0.68, 1]}
        style={[styles.topFade, { height: Math.max(18, topChromeInset + 18) }]}
      />

      {overlayEnabled ? (
        <Reanimated.View
          accessibilityLabel="Message composer"
          pointerEvents="box-none"
          testID="structured-chat-composer-overlay"
          style={[styles.stickyOverlay, stickyStyle]}
        >
          <View
            collapsable={false}
            nativeID={composerNativeId}
            onLayout={handleComposerLayout}
            style={styles.overlayContent}
          >
            {floatingAction}
            {composer}
          </View>
        </Reanimated.View>
      ) : null}

      {portal}
    </View>
  );
}

function acceptNativeKeyboardSample(
  keyboardLifecycleGate: SharedValue<StructuredChatKeyboardLifecycleGate>,
  sourceRevision: number,
  height: number,
  progress: number,
  updatesGeometry: boolean,
  settled = false,
) {
  "worklet";
  keyboardLifecycleGate.value = reduceStructuredChatKeyboardLifecycleGate(
    keyboardLifecycleGate.value,
    {
      type: "native_sample",
      sourceRevision,
      height,
      progress,
      updatesGeometry,
      settled,
    },
  );
}

const styles = StyleSheet.create({
  chatCanvas: {
    flex: 1,
    minHeight: 0,
    overflow: "hidden",
    position: "relative",
  },
  timelineCanvas: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
  },
  stickyOverlay: {
    position: "absolute",
    right: 0,
    bottom: 0,
    left: 0,
    zIndex: 5,
  },
  overlayContent: {
    position: "relative",
    zIndex: 2,
  },
  topFade: {
    position: "absolute",
    top: 0,
    right: 0,
    left: 0,
    height: 18,
    zIndex: 2,
  },
});
