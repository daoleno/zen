import { useCallback, useEffect, useRef, useState } from 'react';
import { WebView, WebViewMessageEvent } from 'react-native-webview';
import type { RenderState } from 'zen-terminal-vt';
import type { TerminalThemePalette } from '../../constants/terminalThemes';
import { useGhosttyCoreTerminal, type GhosttyGridSize } from './useGhosttyCoreTerminal';
import { useTerminalSession } from './useTerminalSession';
import type { TerminalInputHandleRef } from './TerminalInputHandler';

type BridgeMessage =
  | { type: 'ready' }
  | { type: 'input'; data: string }
  | { type: 'resize'; cols: number; rows: number; cellWidth: number; cellHeight: number }
  | { type: 'focusInput' }
  | { type: 'scroll'; lines: number }
  | { type: 'ctrlConsumed' }
  | { type: 'tabSwipeProgress'; deltaX: number; active: boolean }
  | { type: 'tabSwipe'; direction: 'next' | 'prev' }
  | { type: 'requestSelection' };

type RendererStateMessage =
  | { type: 'renderState'; state: RenderState }
  | { type: 'theme'; theme: TerminalThemePalette }
  | { type: 'scrollState'; atBottom: boolean }
  | { type: 'ctrlState'; armed: boolean }
  | { type: 'selectionText'; text: string };

const REPLACEABLE_PENDING_TYPES: readonly RendererStateMessage['type'][] = [
  'renderState',
  'theme',
  'scrollState',
  'ctrlState',
  'selectionText',
];

function trimHistoryToViewport(data: string, rows: number): string {
  if (!data || rows <= 0) {
    return data;
  }

  const normalized = data.replace(/\r\n/g, '\n').replace(/\r/g, '\n');
  const lines = normalized.split('\n');
  const trailingEmpty = lines[lines.length - 1] === '' ? 1 : 0;
  const keepCount = rows + trailingEmpty;

  if (lines.length <= keepCount) {
    return data;
  }

  const trimmed = lines.slice(-keepCount).join('\n');
  return data.includes('\r\n') ? trimmed.replace(/\n/g, '\r\n') : trimmed;
}

interface UseGhosttyTerminalControllerArgs {
  serverId: string;
  targetId: string;
  backend: string;
  theme: TerminalThemePalette;
  ctrlArmed: boolean;
  onCtrlArmedChange?: (next: boolean) => void;
  onTabSwipeProgress?: (deltaX: number, active: boolean) => void;
  onTabSwipe?: (direction: 'next' | 'prev') => void;
}

export function useGhosttyTerminalController({
  serverId,
  targetId,
  backend,
  theme,
  ctrlArmed,
  onCtrlArmedChange,
  onTabSwipeProgress,
  onTabSwipe,
}: UseGhosttyTerminalControllerArgs) {
  const webviewRef = useRef<WebView>(null);
  const inputRef = useRef<TerminalInputHandleRef>(null);
  const webReadyRef = useRef(false);
  const pendingRef = useRef<RendererStateMessage[]>([]);
  const renderFrameRef = useRef<number>(0);
  const gridRef = useRef<GhosttyGridSize | null>(null);
  const [ready, setReady] = useState(false);
  const [scrolledUp, setScrolledUp] = useState(false);

  const ghostty = useGhosttyCoreTerminal();

  const injectRendererMessage = useCallback((payload: RendererStateMessage) => {
    let script = '';

    if (payload.type === 'renderState') {
      script = `window.__zenRenderState && window.__zenRenderState(${JSON.stringify(payload.state)}); true;`;
    } else if (payload.type === 'theme') {
      script = `window.__zenTheme && window.__zenTheme(${JSON.stringify(payload.theme)}); true;`;
    } else if (payload.type === 'scrollState') {
      script = `window.__zenScrollState && window.__zenScrollState(${JSON.stringify({ atBottom: payload.atBottom })}); true;`;
    } else if (payload.type === 'ctrlState') {
      script = `window.__zenCtrlState && window.__zenCtrlState(${JSON.stringify({ armed: payload.armed })}); true;`;
    } else if (payload.type === 'selectionText') {
      script = `window.__zenSelectionText && window.__zenSelectionText(${JSON.stringify({ text: payload.text })}); true;`;
    }

    if (script) {
      webviewRef.current?.injectJavaScript(script);
    }
  }, []);

  const postToRenderer = useCallback((payload: RendererStateMessage) => {
    if (!webReadyRef.current) {
      if (REPLACEABLE_PENDING_TYPES.includes(payload.type)) {
        pendingRef.current = pendingRef.current.filter((pending) => pending.type !== payload.type);
      }
      pendingRef.current.push(payload);
      return;
    }
    injectRendererMessage(payload);
  }, [injectRendererMessage]);

  const runRendererCommand = useCallback((command: 'blur' | 'resumeInput' | 'scrollToBottom') => {
    if (!webReadyRef.current) {
      return;
    }

    let script = '';
    if (command === 'blur') {
      script = 'window.__zenBlur && window.__zenBlur(); true;';
    } else if (command === 'resumeInput') {
      script = 'window.__zenResumeInput && window.__zenResumeInput(); true;';
    } else if (command === 'scrollToBottom') {
      script = 'window.__zenScrollToBottom && window.__zenScrollToBottom(); true;';
    }

    if (script) {
      webviewRef.current?.injectJavaScript(script);
    }
  }, []);

  const flushRenderState = useCallback(() => {
    renderFrameRef.current = 0;
    const state = ghostty.consumeRenderState();
    if (!state) {
      return;
    }
    postToRenderer({ type: 'renderState', state });
  }, [ghostty, postToRenderer]);

  const scheduleRenderState = useCallback(() => {
    if (renderFrameRef.current) {
      return;
    }
    renderFrameRef.current = requestAnimationFrame(flushRenderState);
  }, [flushRenderState]);

  useEffect(() => {
    return () => {
      if (renderFrameRef.current) {
        cancelAnimationFrame(renderFrameRef.current);
        renderFrameRef.current = 0;
      }
    };
  }, []);

  useEffect(() => {
    postToRenderer({ type: 'theme', theme });
  }, [postToRenderer, theme]);

  useEffect(() => {
    postToRenderer({ type: 'scrollState', atBottom: !scrolledUp });
  }, [postToRenderer, scrolledUp]);

  useEffect(() => {
    postToRenderer({ type: 'ctrlState', armed: ctrlArmed });
  }, [ctrlArmed, postToRenderer]);

  const session = useTerminalSession(serverId, targetId, backend, {
    onHistory: ({ data }) => {
      const grid = gridRef.current;
      if (!grid || !ghostty.ensureTerminal(grid)) {
        return;
      }
      ghostty.writeOutput(trimHistoryToViewport(data, grid.rows));
      scheduleRenderState();
    },
    onOutput: ({ data }) => {
      const grid = gridRef.current;
      if (!grid || !ghostty.ensureTerminal(grid)) {
        return;
      }
      ghostty.writeOutput(data);
      scheduleRenderState();
    },
    onScrollState: ({ at_bottom }) => {
      setScrolledUp(!at_bottom);
      if (!at_bottom) {
        inputRef.current?.blur();
      }
    },
    onExit: ({ exit_code }) => {
      const grid = gridRef.current;
      if (!grid || !ghostty.ensureTerminal(grid)) {
        return;
      }
      ghostty.writeOutput(`\r\n[zen] session exited with code ${exit_code}\r\n`);
      scheduleRenderState();
    },
    onError: ({ message }) => {
      const grid = gridRef.current;
      if (!grid || !ghostty.ensureTerminal(grid)) {
        return;
      }
      ghostty.writeOutput(`\r\n[zen] ${message}\r\n`);
      scheduleRenderState();
    },
  });
  const { cancelScroll, resize, scroll, sendInput } = session;

  const focus = useCallback(() => {
    inputRef.current?.focus();
  }, []);

  const blur = useCallback(() => {
    inputRef.current?.blur();
    runRendererCommand('blur');
  }, [runRendererCommand]);

  const resumeInput = useCallback(() => {
    cancelScroll();
    setScrolledUp(false);
    runRendererCommand('resumeInput');
    inputRef.current?.focus();
  }, [cancelScroll, runRendererCommand]);

  const scrollToBottom = useCallback(() => {
    cancelScroll();
    setScrolledUp(false);
    runRendererCommand('scrollToBottom');
    inputRef.current?.focus();
  }, [cancelScroll, runRendererCommand]);

  const onInput = useCallback((data: string) => {
    sendInput(data);
  }, [sendInput]);

  const onCtrlConsumed = useCallback(() => {
    onCtrlArmedChange?.(false);
  }, [onCtrlArmedChange]);

  const onRendererLoadStart = useCallback(() => {
    webReadyRef.current = false;
    pendingRef.current = [];
    setReady(false);
  }, []);

  const onRendererMessage = useCallback((event: WebViewMessageEvent) => {
    try {
      const payload = JSON.parse(event.nativeEvent.data) as BridgeMessage;

      if (payload.type === 'ready') {
        webReadyRef.current = true;
        setReady(true);

        postToRenderer({ type: 'theme', theme });
        postToRenderer({ type: 'scrollState', atBottom: !scrolledUp });
        postToRenderer({ type: 'ctrlState', armed: ctrlArmed });

        const queued = pendingRef.current;
        pendingRef.current = [];
        for (const message of queued) {
          injectRendererMessage(message);
        }
        scheduleRenderState();
        return;
      }

      if (payload.type === 'resize') {
        const nextGrid: GhosttyGridSize = {
          cols: payload.cols,
          rows: payload.rows,
          cellWidth: payload.cellWidth,
          cellHeight: payload.cellHeight,
        };
        gridRef.current = nextGrid;
        if (ghostty.ensureTerminal(nextGrid)) {
          scheduleRenderState();
        }
        resize(payload.cols, payload.rows);
        return;
      }

      if (payload.type === 'input') {
        sendInput(payload.data);
        return;
      }

      if (payload.type === 'focusInput') {
        inputRef.current?.focus();
        return;
      }

      if (payload.type === 'scroll') {
        scroll(payload.lines);
        return;
      }

      if (payload.type === 'ctrlConsumed') {
        onCtrlArmedChange?.(false);
        return;
      }

      if (payload.type === 'tabSwipeProgress') {
        onTabSwipeProgress?.(payload.deltaX, payload.active);
        return;
      }

      if (payload.type === 'tabSwipe') {
        onTabSwipe?.(payload.direction);
        return;
      }

      if (payload.type === 'requestSelection') {
        postToRenderer({
          type: 'selectionText',
          text: ghostty.getVisibleTextSnapshot(),
        });
      }
    } catch {
      // Ignore malformed bridge messages.
    }
  }, [
    ctrlArmed,
    ghostty,
    injectRendererMessage,
    onCtrlArmedChange,
    onTabSwipe,
    onTabSwipeProgress,
    postToRenderer,
    resize,
    scheduleRenderState,
    scroll,
    scrolledUp,
    sendInput,
    theme,
  ]);

  return {
    webviewRef,
    inputRef,
    ready,
    scrolledUp,
    onInput,
    onCtrlConsumed,
    onRendererLoadStart,
    onRendererMessage,
    sendInput(data: string, options?: { focus?: boolean }) {
      sendInput(data);
      if (options?.focus !== false) {
        inputRef.current?.focus();
      }
    },
    focus,
    blur,
    resumeInput,
    scrollToBottom,
  };
}
