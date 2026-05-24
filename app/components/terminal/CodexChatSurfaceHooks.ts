import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  type FlatList,
  Keyboard,
  type LayoutChangeEvent,
  Platform,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  type TextInput,
} from "react-native";
import type { ConnectionState } from "../../store/agents";
import type { CodexSlashCommand } from "../../services/websocket";
import {
  buildCodexComposerPresentation,
  type CodexComposerPresentationInput,
} from "./CodexChatSurfaceModel";
import type { ZenTimelineItem } from "./CodexTimelineItemView";

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
  requestRunning,
  attachmentCount,
  sending,
  startingNewChat,
  interrupting,
  canSend,
  composerFocused,
  actionMenuPinned,
  safeAreaTop,
  safeAreaBottom,
}: UseCodexComposerPresentationInput) {
  return useMemo(
    () =>
      buildCodexComposerPresentation({
        draft,
        slashCommands,
        connectionState,
        requestRunning,
        attachmentCount,
        sending,
        startingNewChat,
        interrupting,
        canSend,
        composerFocused,
        actionMenuPinned,
        safeAreaTop,
        safeAreaBottom,
        isAndroid: Platform.OS === "android",
      }),
    [
      attachmentCount,
      canSend,
      composerFocused,
      actionMenuPinned,
      connectionState,
      draft,
      interrupting,
      requestRunning,
      safeAreaBottom,
      safeAreaTop,
      sending,
      startingNewChat,
      slashCommands,
    ],
  );
}

export function usePinnedTimeline(itemCount: number) {
  const scrollRef = useRef<FlatList<ZenTimelineItem>>(null);
  const followLatestRef = useRef(true);
  const userDraggingRef = useRef(false);
  const userMomentumRef = useRef(false);
  const scrollRequestSeqRef = useRef(0);
  const contentHeightRef = useRef(0);
  const viewportHeightRef = useRef(0);
  const [showJumpToLatest, setShowJumpToLatest] = useState(false);

  const updateJumpButton = useCallback(() => {
    setShowJumpToLatest(!followLatestRef.current && itemCount > 0);
  }, [itemCount]);

  const followLatest = useCallback(() => {
    followLatestRef.current = true;
    setShowJumpToLatest(false);
  }, []);

  const detachFromLatest = useCallback(() => {
    followLatestRef.current = false;
    updateJumpButton();
  }, [updateJumpButton]);

  const scrollToLatest = useCallback(
    (animated: boolean = true, delay: number = SCROLL_TO_BOTTOM_LAYOUT_DELAY_MS) => {
      const requestSeq = scrollRequestSeqRef.current + 1;
      scrollRequestSeqRef.current = requestSeq;
      followLatest();
      const scroll = (nextAnimated: boolean) => {
        if (scrollRequestSeqRef.current !== requestSeq) {
          return;
        }
        scrollRef.current?.scrollToEnd({ animated: nextAnimated });
      };
      const scheduleScroll = (nextDelay: number, nextAnimated: boolean) => {
        if (nextDelay <= 0) {
          requestAnimationFrame(() => scroll(nextAnimated));
          return;
        }
        setTimeout(() => scroll(nextAnimated), nextDelay);
      };
      scheduleScroll(delay, animated);
    },
    [followLatest],
  );

  const pinToBottomIfNeeded = useCallback(
    (animated: boolean = false, delay: number = 0) => {
      if (followLatestRef.current) {
        scrollToLatest(animated, delay);
      }
    },
    [scrollToLatest],
  );

  const resetForConversation = useCallback(() => {
    followLatestRef.current = true;
    userDraggingRef.current = false;
    userMomentumRef.current = false;
    scrollRequestSeqRef.current += 1;
    contentHeightRef.current = 0;
    viewportHeightRef.current = 0;
    setShowJumpToLatest(false);
  }, []);

  const updateScrollPosition = useCallback((
    event: NativeSyntheticEvent<NativeScrollEvent>,
    userDriven: boolean,
  ) => {
    const {
      contentOffset,
      contentSize,
      layoutMeasurement,
    } = event.nativeEvent;
    contentHeightRef.current = contentSize.height;
    viewportHeightRef.current = layoutMeasurement.height;
    const distanceFromLatest = Math.max(
      0,
      contentSize.height - layoutMeasurement.height - contentOffset.y,
    );
    if (distanceFromLatest <= SCROLL_BOTTOM_THRESHOLD) {
      followLatest();
      return;
    }
    if (userDriven) {
      detachFromLatest();
      return;
    }
    updateJumpButton();
  }, [detachFromLatest, followLatest, updateJumpButton]);

  const handleScroll = useCallback(
    (event: NativeSyntheticEvent<NativeScrollEvent>) => {
      updateScrollPosition(
        event,
        userDraggingRef.current || userMomentumRef.current,
      );
    },
    [updateScrollPosition],
  );

  const handleScrollBeginDrag = useCallback(() => {
    userDraggingRef.current = true;
    scrollRequestSeqRef.current += 1;
  }, []);

  const handleScrollEndDrag = useCallback(
    (event: NativeSyntheticEvent<NativeScrollEvent>) => {
      updateScrollPosition(event, true);
      userDraggingRef.current = false;
    },
    [updateScrollPosition],
  );

  const handleMomentumScrollBegin = useCallback(() => {
    userMomentumRef.current = true;
  }, []);

  const handleMomentumScrollEnd = useCallback(
    (event: NativeSyntheticEvent<NativeScrollEvent>) => {
      updateScrollPosition(event, true);
      userMomentumRef.current = false;
    },
    [updateScrollPosition],
  );

  const handleContentSizeChange = useCallback((_: number, height: number) => {
    contentHeightRef.current = height;
    if (followLatestRef.current) {
      scrollToLatest(false, 0);
    } else if (
      itemCount > 0 &&
      height - viewportHeightRef.current > SCROLL_BOTTOM_THRESHOLD
    ) {
      setShowJumpToLatest(true);
    }
  }, [itemCount, scrollToLatest]);

  const handleLayout = useCallback((event: LayoutChangeEvent) => {
    viewportHeightRef.current = event.nativeEvent.layout.height;
    if (followLatestRef.current) {
      pinToBottomIfNeeded(false);
    }
  }, [pinToBottomIfNeeded]);

  useEffect(() => {
    if (itemCount > 0 && followLatestRef.current) {
      scrollToLatest(false, 0);
    }
  }, [itemCount, scrollToLatest]);

  return {
    scrollRef,
    showJumpToLatest,
    scrollToLatest,
    pinToBottomIfNeeded,
    resetForConversation,
    handleScroll,
    handleScrollBeginDrag,
    handleScrollEndDrag,
    handleMomentumScrollBegin,
    handleMomentumScrollEnd,
    handleContentSizeChange,
    handleLayout,
  };
}

export function useCodexComposerInput({
  enabled,
}: {
  enabled: boolean;
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

  const blur = useCallback(() => {
    releaseFocusLock();
    inputRef.current?.blur();
    Keyboard.dismiss();
    setFocused(false);
  }, [releaseFocusLock]);

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
    });
    return () => {
      hideSubscription.remove();
      showSubscription.remove();
      releaseFocusLock();
    };
  }, [releaseFocusLock, restoreFocusIfLocked]);

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
    blur,
    handleFocus,
    handleBlur,
    handleInputStart,
  };
}
