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
const COMPOSER_FOCUS_LOCK_MS = 1000;
const COMPOSER_REFOCUS_DELAYS_MS = [0, 60, 140, 280, 520, 820] as const;
const TEXT_SELECTION_ANCHOR_SETTLE_MS = 30000;
const TEXT_SELECTION_ANCHOR_MAX_MS = 60000;

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
  elapsedLabel,
  actionMenuPinned,
  safeAreaTop,
  safeAreaBottom,
  placeholder,
  keyboardVerticalOffset,
  minimalComposer,
  showAttachmentControl,
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
        elapsedLabel,
        actionMenuPinned,
        safeAreaTop,
        safeAreaBottom,
        isAndroid: Platform.OS === "android",
        placeholder,
        keyboardVerticalOffset,
        minimalComposer,
        showAttachmentControl,
      }),
    [
      attachmentCount,
      canSend,
      elapsedLabel,
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
      placeholder,
      keyboardVerticalOffset,
      minimalComposer,
      showAttachmentControl,
    ],
  );
}

export function usePinnedTimeline(itemCount: number, resetKey: string) {
  const scrollRef = useRef<FlatList<ZenTimelineItem>>(null);
  const resetKeyRef = useRef(resetKey);
  const followLatestRef = useRef(true);
  const userDraggingRef = useRef(false);
  const userMomentumRef = useRef(false);
  const scrollRequestSeqRef = useRef(0);
  const distanceFromLatestRef = useRef(0);
  const textSelectionActiveRef = useRef(false);
  const textSelectionTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );
  const [showJumpToLatest, setShowJumpToLatest] = useState(false);
  const [textSelectable, setTextSelectable] = useState(false);

  const updateJumpButton = useCallback(() => {
    setShowJumpToLatest(!followLatestRef.current && itemCount > 0);
  }, [itemCount]);

  const clearTextSelectionTimer = useCallback(() => {
    if (!textSelectionTimerRef.current) {
      return;
    }
    clearTimeout(textSelectionTimerRef.current);
    textSelectionTimerRef.current = null;
  }, []);

  const disableTextSelection = useCallback(() => {
    setTextSelectable(false);
  }, []);

  const resumeImplicitAnchorAfterTextSelection = useCallback(() => {
    clearTextSelectionTimer();
    disableTextSelection();
    textSelectionActiveRef.current = false;
    updateJumpButton();
  }, [clearTextSelectionTimer, disableTextSelection, updateJumpButton]);

  const scheduleTextSelectionAnchorResume = useCallback((delay: number) => {
    clearTextSelectionTimer();
    textSelectionTimerRef.current = setTimeout(() => {
      textSelectionTimerRef.current = null;
      textSelectionActiveRef.current = false;
      disableTextSelection();
      updateJumpButton();
    }, delay);
  }, [clearTextSelectionTimer, disableTextSelection, updateJumpButton]);

  const implicitAnchorSuspended = useCallback(() => (
    textSelectionActiveRef.current ||
    userDraggingRef.current ||
    userMomentumRef.current
  ), []);

  const followLatest = useCallback(() => {
    followLatestRef.current = true;
    distanceFromLatestRef.current = 0;
    setShowJumpToLatest(false);
  }, []);

  const detachFromLatest = useCallback(() => {
    followLatestRef.current = false;
    updateJumpButton();
  }, [updateJumpButton]);

  const scrollToLatest = useCallback(
    (animated: boolean = true, delay: number = 0) => {
      const requestSeq = scrollRequestSeqRef.current + 1;
      scrollRequestSeqRef.current = requestSeq;
      resumeImplicitAnchorAfterTextSelection();
      followLatest();
      const scroll = (nextAnimated: boolean) => {
        if (scrollRequestSeqRef.current !== requestSeq) {
          return;
        }
        if (!scrollRef.current) {
          return;
        }
        scrollRef.current.scrollToOffset({
          offset: 0,
          animated: nextAnimated,
        });
      };
      if (delay <= 0) {
        scroll(animated);
        return;
      }
      setTimeout(() => scroll(animated), delay);
    },
    [followLatest, resumeImplicitAnchorAfterTextSelection],
  );

  const pinToBottomIfNeeded = useCallback(
    (animated: boolean = false, delay: number = 0) => {
      if (implicitAnchorSuspended()) {
        return;
      }
      if (followLatestRef.current) {
        scrollToLatest(animated, delay);
      }
    },
    [implicitAnchorSuspended, scrollToLatest],
  );

  const resetForConversation = useCallback(() => {
    resumeImplicitAnchorAfterTextSelection();
    followLatestRef.current = true;
    userDraggingRef.current = false;
    userMomentumRef.current = false;
    scrollRequestSeqRef.current += 1;
    distanceFromLatestRef.current = 0;
    setShowJumpToLatest(false);
  }, [resumeImplicitAnchorAfterTextSelection]);

  const handleTextSelectionGestureStart = useCallback(() => {
    setTextSelectable(true);
    textSelectionActiveRef.current = true;
    scrollRequestSeqRef.current += 1;
    scheduleTextSelectionAnchorResume(TEXT_SELECTION_ANCHOR_MAX_MS);
  }, [scheduleTextSelectionAnchorResume]);

  const handleTextSelectionGestureEnd = useCallback(() => {
    if (!textSelectionActiveRef.current) {
      setTextSelectable(false);
      return;
    }
    scheduleTextSelectionAnchorResume(TEXT_SELECTION_ANCHOR_SETTLE_MS);
  }, [scheduleTextSelectionAnchorResume]);

  const updateScrollPosition = useCallback((
    event: NativeSyntheticEvent<NativeScrollEvent>,
    userDriven: boolean,
  ) => {
    const {
      contentOffset,
    } = event.nativeEvent;
    const distanceFromLatest = Math.max(0, contentOffset.y);
    distanceFromLatestRef.current = distanceFromLatest;
    if (textSelectionActiveRef.current && !userDriven) {
      updateJumpButton();
      return;
    }
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
    if (!textSelectionActiveRef.current) {
      resumeImplicitAnchorAfterTextSelection();
    }
    userDraggingRef.current = true;
    scrollRequestSeqRef.current += 1;
  }, [resumeImplicitAnchorAfterTextSelection]);

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
    if (itemCount === 0 || height <= 0) {
      followLatest();
      return;
    }
    if (implicitAnchorSuspended()) {
      updateJumpButton();
      return;
    }
    if (followLatestRef.current && distanceFromLatestRef.current > 1) {
      scrollToLatest(false, 0);
      return;
    }
    if (!followLatestRef.current) {
      setShowJumpToLatest(true);
    }
  }, [
    followLatest,
    implicitAnchorSuspended,
    itemCount,
    scrollToLatest,
    updateJumpButton,
  ]);

  const handleLayout = useCallback((_event: LayoutChangeEvent) => {
    if (itemCount === 0) {
      followLatest();
      return;
    }
    if (implicitAnchorSuspended()) {
      updateJumpButton();
      return;
    }
    if (followLatestRef.current && distanceFromLatestRef.current > 1) {
      scrollToLatest(false, 0);
      return;
    }
    updateJumpButton();
  }, [
    followLatest,
    implicitAnchorSuspended,
    itemCount,
    scrollToLatest,
    updateJumpButton,
  ]);

  useEffect(
    () => () => {
      clearTextSelectionTimer();
    },
    [clearTextSelectionTimer],
  );

  useEffect(() => {
    if (resetKeyRef.current === resetKey) {
      return;
    }
    resetKeyRef.current = resetKey;
    resetForConversation();
  }, [resetForConversation, resetKey]);

  useEffect(() => {
    if (itemCount === 0) {
      followLatest();
      return;
    }
    if (implicitAnchorSuspended()) {
      updateJumpButton();
      return;
    }
    if (followLatestRef.current && distanceFromLatestRef.current > 1) {
      scrollToLatest(false, 0);
      return;
    }
    updateJumpButton();
  }, [
    followLatest,
    implicitAnchorSuspended,
    itemCount,
    resetKey,
    scrollToLatest,
    updateJumpButton,
  ]);

  return {
    scrollRef,
    showJumpToLatest,
    textSelectable,
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
    handleTextSelectionGestureStart,
    handleTextSelectionGestureEnd,
  };
}

export function useRelativeTimeLabel(targetTimestamp?: string) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!targetTimestamp) {
      return;
    }
    setNow(Date.now());
    const timer = setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => clearInterval(timer);
  }, [targetTimestamp]);

  return useMemo(() => {
    if (!targetTimestamp) {
      return "";
    }
    const timestamp = new Date(targetTimestamp).getTime();
    if (!Number.isFinite(timestamp)) {
      return "";
    }
    const elapsed = Math.max(0, Math.floor((now - timestamp) / 1000));
    if (elapsed < 60) {
      return `${Math.max(1, elapsed)}s`;
    }
    const minutes = Math.floor(elapsed / 60);
    if (minutes < 60) {
      return `${minutes}m`;
    }
    const hours = Math.floor(minutes / 60);
    if (hours < 24) {
      return `${hours}h`;
    }
    const days = Math.floor(hours / 24);
    return `${days}d`;
  }, [now, targetTimestamp]);
}

export function useElapsedDurationLabel(
  startTimestamp?: string,
  active: boolean = false,
) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!startTimestamp || !active) {
      return;
    }
    setNow(Date.now());
    const timer = setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => clearInterval(timer);
  }, [active, startTimestamp]);

  return useMemo(() => {
    if (!startTimestamp || !active) {
      return "";
    }
    const timestamp = new Date(startTimestamp).getTime();
    if (!Number.isFinite(timestamp)) {
      return "";
    }
    const elapsed = Math.max(0, Math.floor((now - timestamp) / 1000));
    return formatElapsedDuration(elapsed);
  }, [active, now, startTimestamp]);
}

function formatElapsedDuration(totalSeconds: number) {
  const seconds = Math.max(0, totalSeconds);
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remainder = seconds % 60;
  const paddedSeconds = remainder.toString().padStart(2, "0");

  if (hours > 0) {
    return `${hours}h ${minutes.toString().padStart(2, "0")}m ${paddedSeconds}s`;
  }
  if (minutes > 0) {
    return `${minutes}m ${paddedSeconds}s`;
  }
  return `${remainder}s`;
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

  const clearNativeText = useCallback(() => {
    inputRef.current?.clear();
    inputRef.current?.setNativeProps?.({ text: "" });
  }, []);

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
    clearNativeText,
    handleFocus,
    handleBlur,
    handleInputStart,
  };
}
