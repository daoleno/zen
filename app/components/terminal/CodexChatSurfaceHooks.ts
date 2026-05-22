import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  type FlatList,
  Keyboard,
  Platform,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  type TextInput,
} from "react-native";
import type { AgentStatus } from "../../constants/tokens";
import type { ConnectionState } from "../../store/agents";
import type { CodexSlashCommand } from "../../services/websocket";
import {
  buildCodexComposerPresentation,
  type CodexComposerPresentationInput,
} from "./CodexChatSurfaceModel";

const SCROLL_BOTTOM_THRESHOLD = 96;
export const SCROLL_TO_BOTTOM_LAYOUT_DELAY_MS = 30;
const COMPOSER_FOCUS_LOCK_MS = 1000;
const COMPOSER_REFOCUS_DELAYS_MS = [0, 60, 140, 280, 520, 820] as const;

type UseCodexComposerPresentationInput = Omit<
  CodexComposerPresentationInput,
  "isAndroid"
>;

export function useCodexComposerPresentation({
  draft,
  slashCommands,
  connectionState,
  agentStatus,
  attachmentCount,
  sending,
  canSend,
  composerFocused,
  safeAreaTop,
  safeAreaBottom,
}: UseCodexComposerPresentationInput) {
  return useMemo(
    () =>
      buildCodexComposerPresentation({
        draft,
        slashCommands,
        connectionState,
        agentStatus,
        attachmentCount,
        sending,
        canSend,
        composerFocused,
        safeAreaTop,
        safeAreaBottom,
        isAndroid: Platform.OS === "android",
      }),
    [
      agentStatus,
      attachmentCount,
      canSend,
      composerFocused,
      connectionState,
      draft,
      safeAreaBottom,
      safeAreaTop,
      sending,
      slashCommands,
    ],
  );
}

export function usePinnedTimeline(itemCount: number) {
  const scrollRef = useRef<FlatList>(null);
  const nearBottomRef = useRef(true);
  const contentReadyRef = useRef(false);
  const [showJumpToLatest, setShowJumpToLatest] = useState(false);

  const scrollToLatest = useCallback(
    (animated: boolean = true, delay: number = SCROLL_TO_BOTTOM_LAYOUT_DELAY_MS) => {
      nearBottomRef.current = true;
      setShowJumpToLatest(false);
      const scroll = () => {
        scrollRef.current?.scrollToEnd({ animated });
      };
      if (delay <= 0) {
        scroll();
        return;
      }
      setTimeout(scroll, delay);
    },
    [],
  );

  const pinToBottomIfNeeded = useCallback(
    (animated: boolean = false, delay: number = 0) => {
      if (nearBottomRef.current) {
        scrollToLatest(animated, delay);
      }
    },
    [scrollToLatest],
  );

  const resetForConversation = useCallback(() => {
    nearBottomRef.current = true;
    contentReadyRef.current = false;
    setShowJumpToLatest(false);
  }, []);

  const handleScroll = useCallback(
    (event: NativeSyntheticEvent<NativeScrollEvent>) => {
      const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent;
      const distanceFromBottom =
        contentSize.height - layoutMeasurement.height - contentOffset.y;
      const nearBottom = distanceFromBottom <= SCROLL_BOTTOM_THRESHOLD;
      nearBottomRef.current = nearBottom;
      setShowJumpToLatest(!nearBottom && itemCount > 0);
    },
    [itemCount],
  );

  const handleContentSizeChange = useCallback(() => {
    if (!contentReadyRef.current || nearBottomRef.current) {
      contentReadyRef.current = true;
      scrollToLatest(false, 0);
    } else if (itemCount > 0) {
      setShowJumpToLatest(true);
    }
  }, [itemCount, scrollToLatest]);

  const handleLayout = useCallback(() => {
    if (contentReadyRef.current) {
      pinToBottomIfNeeded(false);
    }
  }, [pinToBottomIfNeeded]);

  return {
    scrollRef,
    showJumpToLatest,
    scrollToLatest,
    pinToBottomIfNeeded,
    resetForConversation,
    handleScroll,
    handleContentSizeChange,
    handleLayout,
  };
}

export function useCodexComposerInput({
  enabled,
  onKeyboardShown,
}: {
  enabled: boolean;
  onKeyboardShown(): void;
}) {
  const inputRef = useRef<TextInput>(null);
  const focusAttemptRef = useRef(0);
  const focusLockUntilRef = useRef(0);
  const blurReleaseTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const refocusTimersRef = useRef<ReturnType<typeof setTimeout>[]>([]);
  const [focused, setFocused] = useState(false);

  const clearBlurReleaseTimer = useCallback(() => {
    if (blurReleaseTimerRef.current) {
      clearTimeout(blurReleaseTimerRef.current);
      blurReleaseTimerRef.current = null;
    }
  }, []);

  const clearRefocusTimers = useCallback(() => {
    refocusTimersRef.current.forEach((timer) => clearTimeout(timer));
    refocusTimersRef.current = [];
  }, []);

  const releaseFocusLock = useCallback(() => {
    focusAttemptRef.current += 1;
    focusLockUntilRef.current = 0;
    clearRefocusTimers();
    clearBlurReleaseTimer();
  }, [clearBlurReleaseTimer, clearRefocusTimers]);

  const restoreFocusIfLocked = useCallback(
    (attempt: number = focusAttemptRef.current) => {
      if (
        enabled &&
        focusAttemptRef.current === attempt &&
        Date.now() <= focusLockUntilRef.current
      ) {
        setFocused(true);
        inputRef.current?.focus();
        return true;
      }
      return false;
    },
    [enabled],
  );

  const focus = useCallback(() => {
    if (!enabled) {
      return;
    }
    const attempt = focusAttemptRef.current + 1;
    focusAttemptRef.current = attempt;
    focusLockUntilRef.current = Date.now() + COMPOSER_FOCUS_LOCK_MS;
    clearRefocusTimers();
    clearBlurReleaseTimer();
    setFocused(true);
    inputRef.current?.focus();
    refocusTimersRef.current = COMPOSER_REFOCUS_DELAYS_MS.map((delay) =>
      setTimeout(() => {
        restoreFocusIfLocked(attempt);
      }, delay),
    );
  }, [clearBlurReleaseTimer, clearRefocusTimers, enabled, restoreFocusIfLocked]);

  const handleFocus = useCallback(() => {
    setFocused(true);
  }, []);

  const handleBlur = useCallback(() => {
    if (Date.now() <= focusLockUntilRef.current && enabled) {
      const attempt = focusAttemptRef.current;
      const timer = setTimeout(() => {
        restoreFocusIfLocked(attempt);
      }, 40);
      refocusTimersRef.current.push(timer);
      return;
    }

    clearBlurReleaseTimer();
    blurReleaseTimerRef.current = setTimeout(() => {
      if (!inputRef.current?.isFocused()) {
        setFocused(false);
      }
      blurReleaseTimerRef.current = null;
    }, 120);
  }, [clearBlurReleaseTimer, enabled, restoreFocusIfLocked]);

  const handleInputStart = useCallback(() => {
    focus();
    return false;
  }, [focus]);

  useEffect(() => {
    const hideSubscription = Keyboard.addListener("keyboardDidHide", () => {
      releaseFocusLock();
      setFocused(false);
    });
    const showSubscription = Keyboard.addListener("keyboardDidShow", () => {
      restoreFocusIfLocked();
      onKeyboardShown();
    });
    return () => {
      hideSubscription.remove();
      showSubscription.remove();
      releaseFocusLock();
    };
  }, [onKeyboardShown, releaseFocusLock, restoreFocusIfLocked]);

  useEffect(() => {
    if (!enabled) {
      releaseFocusLock();
      setFocused(false);
    }
  }, [enabled, releaseFocusLock]);

  return {
    inputRef,
    focused,
    focus,
    handleFocus,
    handleBlur,
    handleInputStart,
  };
}
