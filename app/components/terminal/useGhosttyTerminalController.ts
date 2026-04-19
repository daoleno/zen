import { useCallback, useEffect, useRef, useState } from 'react';
import { WebView, WebViewMessageEvent } from 'react-native-webview';
import type {
  MouseAction,
  MouseButton,
  RenderSnapshot,
} from '../../modules/zen-terminal-vt/src';
import type { TerminalThemePalette } from '../../constants/terminalThemes';
import { useGhosttyCoreTerminal, type GhosttyGridSize } from './useGhosttyCoreTerminal';
import { useTerminalSession } from './useTerminalSession';
import type { TerminalInputHandleRef } from './TerminalInputHandler';

type BridgeMessage =
  | { type: 'ready' }
  | { type: 'resize'; cols: number; rows: number; cellWidth: number; cellHeight: number }
  | { type: 'focusInput' }
  | { type: 'scroll'; lines: number }
  | { type: 'requestSelection' }
  | {
      type: 'mouse';
      action: MouseAction;
      button: MouseButton;
      x: number;
      y: number;
      shift?: boolean;
      ctrl?: boolean;
      alt?: boolean;
      meta?: boolean;
      anyButtonPressed?: boolean;
    };

type TerminalViewportMode = 'live' | 'scrolled';

type RendererStateMessage =
  | { type: 'renderSnapshot'; snapshot: RenderSnapshot }
  | { type: 'theme'; theme: TerminalThemePalette }
  | { type: 'viewportMode'; mode: TerminalViewportMode }
  | { type: 'selectionText'; text: string };

type PendingTerminalEvent =
  | { type: 'history' | 'output'; data: string }
  | { type: 'message'; data: string };

const REPLACEABLE_PENDING_TYPES: readonly RendererStateMessage['type'][] = [
  'renderSnapshot',
  'theme',
  'viewportMode',
  'selectionText',
];

interface UseGhosttyTerminalControllerArgs {
  serverId: string;
  targetId: string;
  backend: string;
  theme: TerminalThemePalette;
  onCtrlArmedChange?: (next: boolean) => void;
}

/**
 * Thin-client controller for the mobile terminal surface.
 *
 * tmux owns remote interaction semantics like pane focus and copy-mode scroll.
 * libghostty owns the terminal screen state and mouse encoding.
 * This hook only translates viewport and input events between those layers.
 */
export function useGhosttyTerminalController({
  serverId,
  targetId,
  backend,
  theme,
  onCtrlArmedChange,
}: UseGhosttyTerminalControllerArgs) {
  const webviewRef = useRef<WebView>(null);
  const inputRef = useRef<TerminalInputHandleRef>(null);
  const webReadyRef = useRef(false);
  const pendingRef = useRef<RendererStateMessage[]>([]);
  const pendingTerminalRef = useRef<PendingTerminalEvent[]>([]);
  const renderFrameRef = useRef<number>(0);
  const gridRef = useRef<GhosttyGridSize | null>(null);
  const viewportModeRef = useRef<TerminalViewportMode>('live');
  const copyRequestRef = useRef(0);
  const [ready, setReady] = useState(false);
  const [viewportMode, setViewportModeState] = useState<TerminalViewportMode>('live');

  const ghostty = useGhosttyCoreTerminal();

  const injectRendererMessage = useCallback((payload: RendererStateMessage) => {
    let script = '';

    if (payload.type === 'renderSnapshot') {
      script = `window.__zenRenderSnapshot && window.__zenRenderSnapshot(${JSON.stringify(payload.snapshot)}); true;`;
    } else if (payload.type === 'theme') {
      script = `window.__zenTheme && window.__zenTheme(${JSON.stringify(payload.theme)}); true;`;
    } else if (payload.type === 'viewportMode') {
      script = `window.__zenViewportMode && window.__zenViewportMode(${JSON.stringify({ mode: payload.mode })}); true;`;
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

  const setViewportMode = useCallback((next: TerminalViewportMode) => {
    viewportModeRef.current = next;
    setViewportModeState((current) => (current === next ? current : next));
  }, []);

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
    const snapshot = ghostty.consumeRenderSnapshot();
    if (!snapshot) {
      return;
    }
    postToRenderer({ type: 'renderSnapshot', snapshot });
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
    ghostty.setTheme(theme);
    postToRenderer({ type: 'theme', theme });
    scheduleRenderState();
  }, [ghostty, postToRenderer, scheduleRenderState, theme]);

  useEffect(() => {
    postToRenderer({ type: 'viewportMode', mode: viewportMode });
  }, [postToRenderer, viewportMode]);

  const session = useTerminalSession(serverId, targetId, backend, {
    onHistory: ({ data }) => {
      const grid = gridRef.current;
      if (!grid || !ghostty.ensureTerminal(grid)) {
        pendingTerminalRef.current.push({ type: 'history', data });
        return;
      }
      ghostty.writeOutput(data);
      scheduleRenderState();
    },
    onOutput: ({ data }) => {
      const grid = gridRef.current;
      if (!grid || !ghostty.ensureTerminal(grid)) {
        pendingTerminalRef.current.push({ type: 'output', data });
        return;
      }
      ghostty.writeOutput(data);
      scheduleRenderState();
    },
    onScrollState: ({ at_bottom }) => {
      const nextMode: TerminalViewportMode = at_bottom ? 'live' : 'scrolled';
      setViewportMode(nextMode);
      if (at_bottom && ghostty.scrollViewportToBottom()) {
        scheduleRenderState();
      }
      if (!at_bottom) {
        inputRef.current?.blur();
      }
    },
    onExit: ({ exit_code }) => {
      const grid = gridRef.current;
      const message = `\r\n[zen] session exited with code ${exit_code}\r\n`;
      if (!grid || !ghostty.ensureTerminal(grid)) {
        pendingTerminalRef.current.push({ type: 'message', data: message });
        return;
      }
      ghostty.writeOutput(message);
      scheduleRenderState();
    },
    onError: ({ message }) => {
      const grid = gridRef.current;
      const formatted = `\r\n[zen] ${message}\r\n`;
      if (!grid || !ghostty.ensureTerminal(grid)) {
        pendingTerminalRef.current.push({ type: 'message', data: formatted });
        return;
      }
      ghostty.writeOutput(formatted);
      scheduleRenderState();
    },
  });
  const { cancelScroll, focusPane, requestCopyBuffer, resize, scroll, sendInput } = session;

  const flushPendingTerminal = useCallback((grid: GhosttyGridSize) => {
    if (!ghostty.ensureTerminal(grid)) {
      return false;
    }

    const pending = pendingTerminalRef.current;
    if (pending.length === 0) {
      return true;
    }

    pendingTerminalRef.current = [];
    for (const event of pending) {
      if (event.type === 'history') {
        ghostty.writeOutput(event.data);
        continue;
      }
      ghostty.writeOutput(event.data);
    }
    scheduleRenderState();
    return true;
  }, [ghostty, scheduleRenderState]);

  const focusPaneAtPoint = useCallback((x: number, y: number) => {
    if (backend !== 'tmux') {
      return;
    }

    const grid = gridRef.current;
    if (!grid || grid.cols <= 0 || grid.rows <= 0 || grid.cellWidth <= 0 || grid.cellHeight <= 0) {
      return;
    }

    const col = Math.max(0, Math.min(grid.cols - 1, Math.floor(x / grid.cellWidth)));
    const row = Math.max(0, Math.min(grid.rows - 1, Math.floor(y / grid.cellHeight)));
    focusPane(col, row);
  }, [backend, focusPane]);

  const enterLiveMode = useCallback((command: 'resumeInput' | 'scrollToBottom') => {
    if (ghostty.scrollViewportToBottom()) {
      scheduleRenderState();
    }
    cancelScroll();
    setViewportMode('live');
    runRendererCommand(command);
    inputRef.current?.focus();
  }, [cancelScroll, ghostty, runRendererCommand, scheduleRenderState, setViewportMode]);

  const focus = useCallback(() => {
    inputRef.current?.focus();
  }, []);

  const blur = useCallback(() => {
    inputRef.current?.blur();
    runRendererCommand('blur');
  }, [runRendererCommand]);

  const resumeInput = useCallback(() => {
    enterLiveMode('resumeInput');
  }, [enterLiveMode]);

  const scrollToBottom = useCallback(() => {
    enterLiveMode('scrollToBottom');
  }, [enterLiveMode]);

  const onInput = useCallback((data: string) => {
    sendInput(data);
  }, [sendInput]);

  const onCtrlConsumed = useCallback(() => {
    onCtrlArmedChange?.(false);
  }, [onCtrlArmedChange]);

  const clearInputMirror = useCallback(() => {
    inputRef.current?.clear();
  }, []);

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
        postToRenderer({ type: 'viewportMode', mode: viewportModeRef.current });

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
        if (flushPendingTerminal(nextGrid)) {
          scheduleRenderState();
        }
        resize(payload.cols, payload.rows);
        return;
      }

      if (payload.type === 'focusInput') {
        if (viewportModeRef.current === 'scrolled') {
          scrollToBottom();
          return;
        }
        inputRef.current?.focus();
        return;
      }

      if (payload.type === 'scroll') {
        if (ghostty.scrollViewport(payload.lines)) {
          if (payload.lines < 0 || viewportModeRef.current === 'scrolled') {
            setViewportMode('scrolled');
          }
          scheduleRenderState();
        }
        scroll(payload.lines);
        return;
      }

      if (payload.type === 'mouse') {
        const shouldInvalidateInput =
          payload.action === 'press' && payload.button === 'left';

        if (shouldInvalidateInput) {
          clearInputMirror();
          focusPaneAtPoint(payload.x, payload.y);
        }

        if (viewportModeRef.current === 'scrolled') {
          return;
        }

        const encoded = ghostty.encodePointer({
          action: payload.action,
          button: payload.button,
          x: payload.x,
          y: payload.y,
          shift: payload.shift,
          ctrl: payload.ctrl,
          alt: payload.alt,
          meta: payload.meta,
          anyButtonPressed: payload.anyButtonPressed,
        });
        if (encoded) {
          if (!shouldInvalidateInput) {
            clearInputMirror();
          }
          sendInput(encoded);
        }
        return;
      }

      if (payload.type === 'requestSelection') {
        clearInputMirror();
        inputRef.current?.blur();
        onCtrlArmedChange?.(false);

        const requestId = copyRequestRef.current + 1;
        copyRequestRef.current = requestId;

        void requestCopyBuffer()
          .then((text) => {
            if (copyRequestRef.current !== requestId) {
              return;
            }
            postToRenderer({
              type: 'selectionText',
              text: text || ghostty.getVisibleTextSnapshot(),
            });
          })
          .catch(() => {
            if (copyRequestRef.current !== requestId) {
              return;
            }
            postToRenderer({
              type: 'selectionText',
              text: ghostty.getVisibleTextSnapshot(),
            });
          });
      }
    } catch {
      // Ignore malformed bridge messages.
    }
  }, [
    ghostty,
    injectRendererMessage,
    postToRenderer,
    resize,
    flushPendingTerminal,
    focusPaneAtPoint,
    clearInputMirror,
    scheduleRenderState,
    scroll,
    scrollToBottom,
    sendInput,
    requestCopyBuffer,
    onCtrlArmedChange,
    theme,
  ]);

  return {
    webviewRef,
    inputRef,
    ready,
    scrolledUp: viewportMode === 'scrolled',
    onInput,
    onCtrlConsumed,
    onRendererLoadStart,
    onRendererMessage,
    sendInput(data: string, options?: { focus?: boolean }) {
      clearInputMirror();
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
