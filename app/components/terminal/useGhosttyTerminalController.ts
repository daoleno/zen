import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import * as Clipboard from 'expo-clipboard';
import { WebView, WebViewMessageEvent } from 'react-native-webview';
import type {
  MouseAction,
  MouseButton,
  RenderSnapshot,
} from '../../modules/zen-terminal-vt/src';
import type { TerminalThemePalette } from '../../constants/terminalThemes';
import { useGhosttyCoreTerminal } from './useGhosttyCoreTerminal';
import { useTerminalSession } from './useTerminalSession';
import type { TerminalInputHandleRef } from './TerminalInputHandler';
import type { SkillsHandoffFailure } from './TerminalSurface.types';
import { TerminalLiveGridOwner } from './terminalLiveGrid';
import type { TerminalScrollCancelReason } from './terminalScrollGesture';
import { isCurrentTerminalRendererGeneration } from './terminalSurfaceBootstrap';
import {
  notifyTmuxClientFocus,
  TerminalScrollCorrelation,
} from './terminalSessionCorrelation';
import { makeSessionKey } from '../../services/sessionKeys';
import {
  skillsTerminalHandoff,
  submitSkillsTerminalHandoff,
  unconfirmedSkillsTerminalHandoff,
  type SkillsTerminalSubmission,
} from '../../services/skillsTerminalHandoff';
import { wsClient } from '../../services/websocket';

type BridgeMessage = { rendererGeneration: number } & (
  | { type: 'ready' }
  | { type: 'bootstrapError'; message: string }
  | { type: 'bootstrapWarning'; message: string }
  | { type: 'runtimeError'; message: string }
  | {
      type: 'resize';
      cols: number;
      rows: number;
      cellWidth: number;
      cellHeight: number;
    }
  | { type: 'focusInput'; sessionId: string | null }
  | { type: 'selectionActive'; active: boolean }
  | { type: 'copyText'; text: string }
  | {
      type: 'scroll';
      sessionId: string | null;
      scrollToken: string | null;
      lines: number;
    }
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
      sessionId: string | null;
    }
);

type RendererCommand =
  | 'blur'
  | 'wakeRenderer'
  | 'resumeInput'
  | 'scrollToBottom';

type RendererStateMessage =
  | { type: 'renderSnapshot'; snapshot: RenderSnapshot }
  | { type: 'theme'; theme: TerminalThemePalette };

interface UseGhosttyTerminalControllerArgs {
  serverId: string;
  targetId: string;
  backend: string;
  theme: TerminalThemePalette;
  rendererGeneration: number;
  onCtrlArmedChange?: (next: boolean) => void;
  onRendererBootstrapFailure?: (message: string, generation: number) => void;
  skillsHandoffToken?: string;
  onSkillsHandoffFailure?: (failure: SkillsHandoffFailure) => void;
}

/**
 * One native terminal path: PTY bytes update Ghostty, and Ghostty updates one
 * live WebView DOM. tmux copy-mode is the only scrollback model.
 */
export function useGhosttyTerminalController({
  serverId,
  targetId,
  backend,
  theme,
  rendererGeneration,
  onCtrlArmedChange,
  onRendererBootstrapFailure,
  skillsHandoffToken,
  onSkillsHandoffFailure,
}: UseGhosttyTerminalControllerArgs) {
  const webviewRef = useRef<WebView>(null);
  const inputRef = useRef<TerminalInputHandleRef>(null);
  const webReadyRef = useRef(false);
  const pendingRef = useRef<RendererStateMessage[]>([]);
  const pendingRendererCommandRef = useRef<RendererCommand | null>(null);
  const renderFrameRef = useRef(0);
  const gridOwnerRef = useRef<TerminalLiveGridOwner | null>(null);
  const scrollCorrelationRef = useRef(new TerminalScrollCorrelation());
  const rendererGenerationRef = useRef(rendererGeneration);
  const skillsHandoffTokenRef = useRef(skillsHandoffToken || '');
  const submittedSkillsCommandRef = useRef<SkillsTerminalSubmission | null>(null);
  if (skillsHandoffToken) {
    skillsHandoffTokenRef.current = skillsHandoffToken;
  }
  rendererGenerationRef.current = rendererGeneration;
  const [readyGeneration, setReadyGeneration] = useState<number | null>(null);
  const [scrolledUp, setScrolledUp] = useState(false);
  const ready = readyGeneration === rendererGeneration;

  const ghostty = useGhosttyCoreTerminal();

  const injectRendererState = useCallback((payload: RendererStateMessage) => {
    const script = payload.type === 'renderSnapshot'
      ? `window.__zenRenderSnapshot && window.__zenRenderSnapshot(${JSON.stringify(payload.snapshot)}); true;`
      : `window.__zenTheme && window.__zenTheme(${JSON.stringify(payload.theme)}); true;`;
    webviewRef.current?.injectJavaScript(script);
  }, []);

  const postToRenderer = useCallback((payload: RendererStateMessage) => {
    if (!webReadyRef.current) {
      pendingRef.current = pendingRef.current.filter(
        (pending) => pending.type !== payload.type,
      );
      pendingRef.current.push(payload);
      return;
    }
    injectRendererState(payload);
  }, [injectRendererState]);

  const injectRendererCommand = useCallback((command: RendererCommand) => {
    const scripts: Record<RendererCommand, string> = {
      blur: 'window.__zenBlur && window.__zenBlur(); true;',
      wakeRenderer: 'window.__zenWakeRenderer && window.__zenWakeRenderer(); true;',
      resumeInput: 'window.__zenResumeInput && window.__zenResumeInput(); true;',
      scrollToBottom: 'window.__zenScrollToBottom && window.__zenScrollToBottom(); true;',
    };
    webviewRef.current?.injectJavaScript(scripts[command]);
  }, []);

  const runRendererCommand = useCallback((command: RendererCommand) => {
    if (!webReadyRef.current) {
      pendingRendererCommandRef.current = command;
      return;
    }
    injectRendererCommand(command);
  }, [injectRendererCommand]);

  const replaceScrollContext = useCallback((
    sessionId: string | null,
    reason: TerminalScrollCancelReason,
  ) => {
    const context = scrollCorrelationRef.current.replace(sessionId);
    if (webReadyRef.current) {
      webviewRef.current?.injectJavaScript(
        `window.__zenSetScrollContext && window.__zenSetScrollContext(${JSON.stringify(context.sessionId)}, ${JSON.stringify(context.token)}, ${JSON.stringify(reason)}); true;`,
      );
    }
  }, []);

  const cancelLocalScroll = useCallback((reason: TerminalScrollCancelReason) => {
    replaceScrollContext(scrollCorrelationRef.current.context.sessionId, reason);
  }, [replaceScrollContext]);

  const flushRenderState = useCallback(() => {
    renderFrameRef.current = 0;
    const frame = ghostty.consumeRenderSnapshot();
    if (frame) {
      postToRenderer({ type: 'renderSnapshot', snapshot: frame.snapshot });
    }
  }, [ghostty, postToRenderer]);

  const scheduleRenderState = useCallback(() => {
    if (!renderFrameRef.current) {
      renderFrameRef.current = requestAnimationFrame(flushRenderState);
    }
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

  const session = useTerminalSession(serverId, targetId, backend, {
    onOpened: ({ sessionId }) => {
      replaceScrollContext(sessionId, 'session-change');
      setScrolledUp(false);
      const attached = gridOwnerRef.current?.attach(sessionId) ?? false;
      if (attached) {
        scheduleRenderState();
        const token = skillsHandoffTokenRef.current;
        if (token) {
          skillsHandoffTokenRef.current = '';
          submittedSkillsCommandRef.current = submitSkillsTerminalHandoff(
            skillsTerminalHandoff,
            makeSessionKey(serverId, targetId),
            token,
            sessionId,
            (input) => wsClient.sendTerminalInput(serverId, sessionId, input),
            (failure) => onSkillsHandoffFailure?.(failure),
          );
        }
      }
      return attached;
    },
    onOutput: ({ session_id, data }) => {
      if (ghostty.writeOutput(session_id, data)) {
        scheduleRenderState();
      }
      if (
        submittedSkillsCommandRef.current?.sessionId === session_id &&
        data.includes(submittedSkillsCommandRef.current.command.command)
      ) {
        submittedSkillsCommandRef.current = null;
      }
    },
    onScrollState: ({ at_bottom }) => {
      setScrolledUp(!at_bottom);
    },
    onSessionInvalidated: (sessionId, reason) => {
      const failure = unconfirmedSkillsTerminalHandoff(
        submittedSkillsCommandRef.current,
        sessionId,
      );
      if (failure) {
        submittedSkillsCommandRef.current = null;
        onSkillsHandoffFailure?.(failure);
      }
      replaceScrollContext(
        null,
        reason === 'disconnect' ? 'disconnect' : 'session-change',
      );
      gridOwnerRef.current?.detach(sessionId ?? undefined);
      setScrolledUp(false);
      inputRef.current?.clear();
      inputRef.current?.blur();
    },
    onExit: ({ session_id, exit_code }) => {
      const message = `\r\n[Zen] session exited with code ${exit_code}\r\n`;
      if (ghostty.writeOutput(session_id, message)) {
        scheduleRenderState();
      }
    },
    onError: ({ session_id, code, message }) => {
      if (!session_id) {
        return;
      }
      if (ghostty.writeOutput(session_id, `\r\n[Zen] ${message}\r\n`)) {
        scheduleRenderState();
      }
      if (code === 'input_failed') {
        const failure = unconfirmedSkillsTerminalHandoff(
          submittedSkillsCommandRef.current,
          session_id,
        );
        if (failure) {
          submittedSkillsCommandRef.current = null;
          onSkillsHandoffFailure?.(failure);
        }
      }
    },
  });

  useEffect(() => {
    return () => {
      const token = skillsHandoffTokenRef.current;
      if (token) {
        skillsTerminalHandoff.revoke(makeSessionKey(serverId, targetId), token);
        skillsHandoffTokenRef.current = '';
      }
    };
  }, [serverId, targetId]);

  const gridOwner = useMemo(() => new TerminalLiveGridOwner({
    requestPtyGrid(cols, rows) {
      session.resize(cols, rows);
    },
    resetGhostty(sessionId, grid) {
      return ghostty.resetTerminal(sessionId, grid);
    },
    resizeGhostty(sessionId, grid) {
      const resized = ghostty.resizeGrid(sessionId, grid);
      if (resized) {
        scheduleRenderState();
      }
      return resized;
    },
  }), [ghostty, scheduleRenderState, session]);
  gridOwnerRef.current = gridOwner;

  const canDeliverInput = useCallback(() => session.canSendInput(), [session]);

  const notifyClientFocus = useCallback(() => {
    return notifyTmuxClientFocus(backend, session.sendInput);
  }, [backend, session]);

  const deliverInput = useCallback((data: string) => {
    cancelLocalScroll('input');
    return session.sendInput(data);
  }, [cancelLocalScroll, session]);

  const focusPaneAtPoint = useCallback((x: number, y: number) => {
    if (backend !== 'tmux') {
      return;
    }
    const grid = ghostty.currentGrid();
    if (!grid) {
      return;
    }
    const col = Math.max(0, Math.min(grid.cols - 1, Math.floor(x / grid.cellWidth)));
    const row = Math.max(0, Math.min(grid.rows - 1, Math.floor(y / grid.cellHeight)));
    session.focusPane(col, row);
  }, [backend, ghostty, session]);

  const focus = useCallback(() => {
    if (canDeliverInput()) {
      cancelLocalScroll('input');
      notifyClientFocus();
      inputRef.current?.focus();
    }
  }, [canDeliverInput, cancelLocalScroll, notifyClientFocus]);

  const blur = useCallback(() => {
    cancelLocalScroll('route-blur');
    inputRef.current?.blur();
    runRendererCommand('blur');
  }, [cancelLocalScroll, runRendererCommand]);

  const wakeRenderer = useCallback(() => {
    runRendererCommand('wakeRenderer');
    scheduleRenderState();
  }, [runRendererCommand, scheduleRenderState]);

  const enterLiveMode = useCallback((command: 'resumeInput' | 'scrollToBottom') => {
    cancelLocalScroll('jump-live');
    if (!notifyClientFocus()) {
      session.cancelScroll();
    }
    setScrolledUp(false);
    runRendererCommand(command);
    if (canDeliverInput()) {
      inputRef.current?.focus();
    }
  }, [canDeliverInput, cancelLocalScroll, notifyClientFocus, runRendererCommand, session]);

  const resumeInput = useCallback(() => {
    wakeRenderer();
    enterLiveMode('resumeInput');
  }, [enterLiveMode, wakeRenderer]);

  const scrollToBottom = useCallback(() => {
    wakeRenderer();
    enterLiveMode('scrollToBottom');
  }, [enterLiveMode, wakeRenderer]);

  const onInput = useCallback((data: string) => {
    deliverInput(data);
  }, [deliverInput]);

  const clearInputMirror = useCallback(() => {
    inputRef.current?.clear();
  }, []);

  const onRendererLoadStart = useCallback((generation: number) => {
    if (!isCurrentTerminalRendererGeneration(
      rendererGenerationRef.current,
      generation,
    )) {
      return;
    }
    webReadyRef.current = false;
    pendingRef.current = [];
    setReadyGeneration(null);
    // A WebView retry/remount loses its DOM but not the sole Ghostty model.
    // Reapplying the same theme marks the native render state fully dirty so
    // the replacement renderer receives a complete current snapshot on ready.
    ghostty.setTheme(theme);
  }, [ghostty, theme]);

  const onRendererMessage = useCallback((event: WebViewMessageEvent) => {
    try {
      const payload = JSON.parse(event.nativeEvent.data) as BridgeMessage;
      if (
        !isCurrentTerminalRendererGeneration(
          rendererGenerationRef.current,
          payload.rendererGeneration,
        )
      ) {
        return;
      }

      if (payload.type === 'ready') {
        webReadyRef.current = true;
        setReadyGeneration(payload.rendererGeneration);
        postToRenderer({ type: 'theme', theme });
        const queued = pendingRef.current;
        pendingRef.current = [];
        queued.forEach(injectRendererState);
        const pendingCommand = pendingRendererCommandRef.current;
        pendingRendererCommandRef.current = null;
        if (pendingCommand) {
          injectRendererCommand(pendingCommand);
        }
        const scrollContext = scrollCorrelationRef.current.context;
        webviewRef.current?.injectJavaScript(
          `window.__zenSetScrollContext && window.__zenSetScrollContext(${JSON.stringify(scrollContext.sessionId)}, ${JSON.stringify(scrollContext.token)}, "session-change"); true;`,
        );
        scheduleRenderState();
        injectRendererCommand('wakeRenderer');
        return;
      }

      if (payload.type === 'bootstrapWarning') {
        console.warn('[Terminal WebView] ' + payload.message);
        return;
      }

      if (payload.type === 'runtimeError') {
        console.error('[Terminal WebView] runtime error: ' + payload.message);
        return;
      }

      if (payload.type === 'bootstrapError') {
        webReadyRef.current = false;
        setReadyGeneration(null);
        const message = payload.message || 'Terminal WebView bootstrap failed.';
        console.error('[Terminal WebView] bootstrap error: ' + message);
        onRendererBootstrapFailure?.(message, payload.rendererGeneration);
        return;
      }

      if (payload.type === 'resize') {
        gridOwner.update({
          cols: payload.cols,
          rows: payload.rows,
          cellWidth: payload.cellWidth,
          cellHeight: payload.cellHeight,
        });
        return;
      }

      if (payload.type === 'focusInput') {
        if (!session.acceptInteractionSession(payload.sessionId)) {
          return;
        }
        cancelLocalScroll('input');
        if (!notifyClientFocus()) {
          session.cancelScroll();
        }
        setScrolledUp(false);
        if (canDeliverInput()) {
          inputRef.current?.focus();
        }
        return;
      }

      if (payload.type === 'selectionActive') {
        if (payload.active) {
          cancelLocalScroll('selection');
          clearInputMirror();
          onCtrlArmedChange?.(false);
        }
        return;
      }

      if (payload.type === 'copyText') {
        void Clipboard.setStringAsync(payload.text);
        return;
      }

      if (payload.type === 'scroll') {
        if (
          !scrollCorrelationRef.current.accept(
            payload.sessionId,
            payload.scrollToken,
          ) ||
          !session.acceptInteractionSession(payload.sessionId)
        ) {
          return;
        }
        if (session.scroll(payload.lines) && payload.lines < 0) {
          setScrolledUp(true);
          inputRef.current?.blur();
        }
        return;
      }

      if (payload.type === 'mouse') {
        if (
          !session.acceptInteractionSession(payload.sessionId) ||
          !canDeliverInput()
        ) {
          return;
        }

        const isLeftPress = payload.action === 'press' && payload.button === 'left';
        if (isLeftPress) {
          cancelLocalScroll('input');
          session.cancelScroll();
          setScrolledUp(false);
          clearInputMirror();
          focusPaneAtPoint(payload.x, payload.y);
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
          if (!isLeftPress) {
            clearInputMirror();
          }
          deliverInput(encoded);
        }
      }
    } catch {
      // Ignore malformed bridge messages.
    }
  }, [
    canDeliverInput,
    cancelLocalScroll,
    clearInputMirror,
    deliverInput,
    focusPaneAtPoint,
    ghostty,
    gridOwner,
    injectRendererCommand,
    injectRendererState,
    notifyClientFocus,
    onCtrlArmedChange,
    onRendererBootstrapFailure,
    postToRenderer,
    replaceScrollContext,
    scheduleRenderState,
    session,
    theme,
  ]);

  return {
    webviewRef,
    inputRef,
    ready,
    readyGeneration,
    scrolledUp,
    onInput,
    onCtrlConsumed() {
      onCtrlArmedChange?.(false);
    },
    onRendererLoadStart,
    onRendererMessage,
    sendInput(data: string, options?: { focus?: boolean }) {
      clearInputMirror();
      const sent = deliverInput(data);
      if (sent && options?.focus !== false) {
        inputRef.current?.focus();
      }
    },
    focus,
    blur,
    wakeRenderer,
    resumeInput,
    scrollToBottom,
  };
}
