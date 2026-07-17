import { useCallback, useEffect, useMemo, useRef } from 'react';
import { wsClient } from '../../services/websocket';
import { TERMINAL_SCROLL_MAX_BATCH_LINES } from './terminalScrollGesture';
import { TerminalSessionCorrelation } from './terminalSessionCorrelation';

type TerminalOpenedPayload = {
  sessionId: string;
  cols: number;
  rows: number;
  backend: string;
};

type Handlers = {
  onOpened?: (payload: TerminalOpenedPayload) => boolean;
  onOutput?: (payload: { session_id: string; data: string }) => void;
  onScrollState?: (payload: {
    session_id: string;
    at_bottom: boolean;
    in_copy_mode: boolean;
    scroll_position: number;
  }) => void;
  onSessionInvalidated?: (
    sessionId: string | null,
    reason: 'disconnect' | 'exit' | 'error' | 'unmount',
  ) => void;
  onExit?: (payload: { session_id: string; exit_code: number }) => void;
  onError?: (payload: { session_id?: string; code?: string; message: string }) => void;
};

export function useTerminalSession(
  serverId: string,
  targetId: string,
  backend: string,
  handlers: Handlers,
) {
  const handlersRef = useRef(handlers);
  const mountedWithGridRef = useRef(false);
  const sizeRef = useRef<{ cols: number; rows: number } | null>(null);
  const reopenOnConnectRef = useRef(false);
  const correlationRef = useRef(new TerminalSessionCorrelation());

  handlersRef.current = handlers;

  useEffect(() => {
    const correlation = correlationRef.current;

    const requestOpen = () => {
      const size = sizeRef.current;
      if (!size || !correlation.beginOpen()) {
        return;
      }
      wsClient.openTerminal(
        serverId,
        targetId,
        backend,
        size.cols,
        size.rows,
      );
    };

    const handleOpened = (payload: {
      serverId: string;
      session_id: string;
      cols: number;
      rows: number;
      backend: string;
    }) => {
      if (payload.serverId !== serverId || !correlation.acceptOpened(payload.session_id)) {
        return;
      }

      const accepted = handlersRef.current.onOpened?.({
        sessionId: payload.session_id,
        cols: payload.cols,
        rows: payload.rows,
        backend: payload.backend,
      }) ?? true;
      if (!accepted) {
        correlation.close(payload.session_id);
        wsClient.closeTerminal(serverId, payload.session_id);
        return;
      }

      reopenOnConnectRef.current = false;
      const requested = sizeRef.current;
      if (
        requested &&
        (requested.cols !== payload.cols || requested.rows !== payload.rows)
      ) {
        wsClient.resizeTerminal(
          serverId,
          payload.session_id,
          requested.cols,
          requested.rows,
        );
      }
    };

    const handleOutput = (payload: {
      serverId: string;
      session_id: string;
      data: string;
    }) => {
      if (
        payload.serverId !== serverId ||
        !correlation.acceptEvent(payload.session_id)
      ) {
        return;
      }
      handlersRef.current.onOutput?.(payload);
    };

    const handleScrollState = (payload: {
      serverId: string;
      session_id: string;
      at_bottom: boolean;
      in_copy_mode: boolean;
      scroll_position: number;
    }) => {
      if (
        payload.serverId !== serverId ||
        !correlation.acceptEvent(payload.session_id)
      ) {
        return;
      }
      handlersRef.current.onScrollState?.(payload);
    };

    const handleExit = (payload: {
      serverId: string;
      session_id: string;
      exit_code: number;
    }) => {
      if (
        payload.serverId !== serverId ||
        !correlation.acceptEvent(payload.session_id)
      ) {
        return;
      }
      handlersRef.current.onExit?.(payload);
      correlation.close(payload.session_id);
      reopenOnConnectRef.current = false;
      handlersRef.current.onSessionInvalidated?.(payload.session_id, 'exit');
    };

    const handleError = (payload: {
      serverId: string;
      session_id?: string;
      code?: string;
      message: string;
    }) => {
      if (payload.serverId !== serverId) {
        return;
      }
      if (payload.session_id && !correlation.acceptEvent(payload.session_id)) {
        return;
      }

      if (payload.code === 'open_failed') {
        correlation.abortOpen();
      }
      if (
        payload.code === 'input_failed' &&
        payload.message.includes('unknown terminal session')
      ) {
        const sessionId = correlation.sessionId;
        correlation.close(sessionId ?? undefined);
        handlersRef.current.onSessionInvalidated?.(sessionId, 'error');
        if (mountedWithGridRef.current && wsClient.isConnected(serverId)) {
          requestOpen();
        } else if (mountedWithGridRef.current) {
          reopenOnConnectRef.current = true;
        }
      }
      handlersRef.current.onError?.(payload);
    };

    const handleConnected = (payload: { serverId: string }) => {
      if (payload.serverId !== serverId) {
        return;
      }
      correlation.connect();
      if (reopenOnConnectRef.current && mountedWithGridRef.current) {
        requestOpen();
      }
    };

    const handleDisconnected = (payload: { serverId: string }) => {
      if (payload.serverId !== serverId || !mountedWithGridRef.current) {
        return;
      }
      const sessionId = correlation.sessionId;
      correlation.disconnect();
      reopenOnConnectRef.current = true;
      handlersRef.current.onSessionInvalidated?.(sessionId, 'disconnect');
    };

    wsClient.on('connected', handleConnected);
    wsClient.on('disconnected', handleDisconnected);
    wsClient.on('terminal_opened', handleOpened);
    wsClient.on('terminal_output', handleOutput);
    wsClient.on('terminal_scroll_state', handleScrollState);
    wsClient.on('terminal_exit', handleExit);
    wsClient.on('terminal_error', handleError);

    return () => {
      wsClient.off('connected', handleConnected);
      wsClient.off('disconnected', handleDisconnected);
      wsClient.off('terminal_opened', handleOpened);
      wsClient.off('terminal_output', handleOutput);
      wsClient.off('terminal_scroll_state', handleScrollState);
      wsClient.off('terminal_exit', handleExit);
      wsClient.off('terminal_error', handleError);
      mountedWithGridRef.current = false;
      reopenOnConnectRef.current = false;
      sizeRef.current = null;
      const sessionId = correlation.sessionId;
      correlation.close();
      handlersRef.current.onSessionInvalidated?.(sessionId, 'unmount');
      if (sessionId) {
        wsClient.closeTerminal(serverId, sessionId);
      }
    };
  }, [backend, serverId, targetId]);

  const sendInput = useCallback((data: string) => {
    const sessionId = correlationRef.current.sessionId;
    if (!data || !correlationRef.current.canInteract || !sessionId) {
      return false;
    }
    wsClient.sendTerminalInput(serverId, sessionId, data);
    return true;
  }, [serverId]);

  const scroll = useCallback((lines: number) => {
    const sessionId = correlationRef.current.sessionId;
    if (!correlationRef.current.canInteract || !sessionId || !Number.isFinite(lines)) {
      return false;
    }
    const boundedLines = Math.max(
      -TERMINAL_SCROLL_MAX_BATCH_LINES,
      Math.min(TERMINAL_SCROLL_MAX_BATCH_LINES, Math.trunc(lines)),
    );
    if (boundedLines === 0) {
      return false;
    }
    wsClient.scrollTerminal(serverId, sessionId, boundedLines);
    return true;
  }, [serverId]);

  const cancelScroll = useCallback(() => {
    const sessionId = correlationRef.current.sessionId;
    if (!correlationRef.current.canInteract || !sessionId) {
      return false;
    }
    wsClient.cancelTerminalScroll(serverId, sessionId);
    return true;
  }, [serverId]);

  const canSendInput = useCallback(() => {
    return correlationRef.current.canInteract;
  }, []);

  const focusPane = useCallback((col: number, row: number) => {
    const sessionId = correlationRef.current.sessionId;
    if (!correlationRef.current.canInteract || !sessionId) {
      return false;
    }
    wsClient.focusTerminalPane(serverId, sessionId, col, row);
    return true;
  }, [serverId]);

  const resize = useCallback((cols: number, rows: number) => {
    const nextCols = Math.trunc(cols);
    const nextRows = Math.trunc(rows);
    if (
      !Number.isFinite(cols) ||
      !Number.isFinite(rows) ||
      nextCols <= 0 ||
      nextRows <= 0
    ) {
      return false;
    }

    const previous = sizeRef.current;
    if (previous?.cols === nextCols && previous.rows === nextRows) {
      return false;
    }
    sizeRef.current = { cols: nextCols, rows: nextRows };
    mountedWithGridRef.current = true;

    const sessionId = correlationRef.current.sessionId;
    if (sessionId && correlationRef.current.canInteract) {
      wsClient.resizeTerminal(serverId, sessionId, nextCols, nextRows);
      return true;
    }
    if (correlationRef.current.beginOpen()) {
      wsClient.openTerminal(
        serverId,
        targetId,
        backend,
        nextCols,
        nextRows,
      );
      return true;
    }
    return false;
  }, [backend, serverId, targetId]);

  const acceptInteractionSession = useCallback((sessionId: string | null) => {
    return typeof sessionId === 'string' &&
      correlationRef.current.acceptEvent(sessionId);
  }, []);

  return useMemo(() => ({
    sendInput,
    scroll,
    cancelScroll,
    canSendInput,
    focusPane,
    resize,
    acceptInteractionSession,
  }), [
    acceptInteractionSession,
    cancelScroll,
    canSendInput,
    focusPane,
    resize,
    scroll,
    sendInput,
  ]);
}
