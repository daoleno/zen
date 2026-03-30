import { useEffect, useRef, useState } from 'react';
import { wsClient } from '../../services/websocket';

type Handlers = {
  onOpen?: (payload: { session_id: string; cols: number; rows: number; backend: string }) => void;
  onHistory?: (payload: { session_id: string; data: string }) => void;
  onOutput?: (payload: { session_id: string; data: string }) => void;
  onScrollState?: (payload: { session_id: string; at_bottom: boolean; in_copy_mode: boolean; scroll_position: number }) => void;
  onExit?: (payload: { session_id: string; exit_code: number }) => void;
  onError?: (payload: { session_id?: string; code?: string; message: string }) => void;
};

export function useTerminalSession(serverId: string, targetId: string, backend: string, handlers: Handlers) {
  const sessionIdRef = useRef<string | null>(null);
  const handlersRef = useRef(handlers);
  const openedRef = useRef(false);
  const sizeRef = useRef<{ cols: number; rows: number } | null>(null);
  const reopenOnConnectRef = useRef(false);
  const [connected, setConnected] = useState(false);

  handlersRef.current = handlers;

  useEffect(() => {
    const requestOpen = () => {
      const size = sizeRef.current;
      if (!size) return;
      wsClient.openTerminal(serverId, targetId, backend, size.cols, size.rows);
    };

    const handleOpened = (payload: { serverId: string; session_id: string; cols: number; rows: number; backend: string }) => {
      if (payload.serverId !== serverId) return;
      sessionIdRef.current = payload.session_id;
      reopenOnConnectRef.current = false;
      setConnected(true);
      handlersRef.current.onOpen?.(payload);
    };
    const handleOutput = (payload: { serverId: string; session_id: string; data: string }) => {
      if (payload.serverId !== serverId) return;
      if (sessionIdRef.current && payload.session_id !== sessionIdRef.current) return;
      handlersRef.current.onOutput?.(payload);
    };
    const handleHistory = (payload: { serverId: string; session_id: string; data: string }) => {
      if (payload.serverId !== serverId) return;
      if (sessionIdRef.current && payload.session_id !== sessionIdRef.current) return;
      handlersRef.current.onHistory?.(payload);
    };
    const handleExit = (payload: { serverId: string; session_id: string; exit_code: number }) => {
      if (payload.serverId !== serverId) return;
      if (sessionIdRef.current && payload.session_id !== sessionIdRef.current) return;
      sessionIdRef.current = null;
      reopenOnConnectRef.current = false;
      setConnected(false);
      handlersRef.current.onExit?.(payload);
    };
    const handleScrollState = (payload: {
      serverId: string;
      session_id: string;
      at_bottom: boolean;
      in_copy_mode: boolean;
      scroll_position: number;
    }) => {
      if (payload.serverId !== serverId) return;
      if (sessionIdRef.current && payload.session_id !== sessionIdRef.current) return;
      handlersRef.current.onScrollState?.(payload);
    };
    const handleError = (payload: { serverId: string; session_id?: string; code?: string; message: string }) => {
      if (payload.serverId !== serverId) return;
      if (payload.session_id && sessionIdRef.current && payload.session_id !== sessionIdRef.current) return;
      if (payload.code === 'input_failed' && payload.message.includes('unknown terminal session')) {
        sessionIdRef.current = null;
        setConnected(false);
        if (openedRef.current && wsClient.isConnected(serverId)) {
          requestOpen();
        } else if (openedRef.current) {
          reopenOnConnectRef.current = true;
        }
      }
      handlersRef.current.onError?.(payload);
    };
    const handleConnected = (payload: { serverId: string }) => {
      if (payload.serverId !== serverId) return;
      if (!reopenOnConnectRef.current || !openedRef.current) return;
      requestOpen();
    };
    const handleDisconnected = (payload: { serverId: string }) => {
      if (payload.serverId !== serverId) return;
      if (!openedRef.current) return;
      sessionIdRef.current = null;
      reopenOnConnectRef.current = true;
      setConnected(false);
    };

    wsClient.on('connected', handleConnected);
    wsClient.on('disconnected', handleDisconnected);
    wsClient.on('terminal_opened', handleOpened);
    wsClient.on('terminal_history', handleHistory);
    wsClient.on('terminal_output', handleOutput);
    wsClient.on('terminal_scroll_state', handleScrollState);
    wsClient.on('terminal_exit', handleExit);
    wsClient.on('terminal_error', handleError);

    return () => {
      wsClient.off('connected', handleConnected);
      wsClient.off('disconnected', handleDisconnected);
      wsClient.off('terminal_opened', handleOpened);
      wsClient.off('terminal_history', handleHistory);
      wsClient.off('terminal_output', handleOutput);
      wsClient.off('terminal_scroll_state', handleScrollState);
      wsClient.off('terminal_exit', handleExit);
      wsClient.off('terminal_error', handleError);
      openedRef.current = false;
      reopenOnConnectRef.current = false;
      sizeRef.current = null;
      const sessionId = sessionIdRef.current;
      sessionIdRef.current = null;
      setConnected(false);
      if (sessionId) {
        wsClient.closeTerminal(serverId, sessionId);
      }
    };
  }, [serverId, targetId, backend]);

  return {
    connected,
    open(cols: number, rows: number) {
      sizeRef.current = { cols, rows };
      if (openedRef.current) return;
      openedRef.current = true;
      wsClient.openTerminal(serverId, targetId, backend, cols, rows);
    },
    sendInput(data: string) {
      const sessionId = sessionIdRef.current;
      if (!sessionId) return;
      wsClient.sendTerminalInput(serverId, sessionId, data);
    },
    scroll(lines: number) {
      const sessionId = sessionIdRef.current;
      if (!sessionId) return;
      wsClient.scrollTerminal(serverId, sessionId, lines);
    },
    cancelScroll() {
      const sessionId = sessionIdRef.current;
      if (!sessionId) return;
      wsClient.cancelTerminalScroll(serverId, sessionId);
    },
    resize(cols: number, rows: number) {
      sizeRef.current = { cols, rows };
      const sessionId = sessionIdRef.current;
      if (!sessionId) {
        if (!openedRef.current) {
          openedRef.current = true;
          wsClient.openTerminal(serverId, targetId, backend, cols, rows);
        }
        return;
      }
      wsClient.resizeTerminal(serverId, sessionId, cols, rows);
    },
  };
}
