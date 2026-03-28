import { useEffect, useRef, useState } from 'react';
import { wsClient } from '../../services/websocket';

type Handlers = {
  onOpen?: (payload: { session_id: string; cols: number; rows: number; backend: string }) => void;
  onHistory?: (payload: { session_id: string; data: string }) => void;
  onOutput?: (payload: { session_id: string; data: string }) => void;
  onExit?: (payload: { session_id: string; exit_code: number }) => void;
  onError?: (payload: { session_id?: string; code?: string; message: string }) => void;
};

export function useTerminalSession(targetId: string, backend: string, handlers: Handlers) {
  const sessionIdRef = useRef<string | null>(null);
  const handlersRef = useRef(handlers);
  const openedRef = useRef(false);
  const [connected, setConnected] = useState(false);

  handlersRef.current = handlers;

  useEffect(() => {
    const handleOpened = (payload: { session_id: string; cols: number; rows: number; backend: string }) => {
      sessionIdRef.current = payload.session_id;
      setConnected(true);
      handlersRef.current.onOpen?.(payload);
    };
    const handleOutput = (payload: { session_id: string; data: string }) => {
      if (sessionIdRef.current && payload.session_id !== sessionIdRef.current) return;
      handlersRef.current.onOutput?.(payload);
    };
    const handleHistory = (payload: { session_id: string; data: string }) => {
      if (sessionIdRef.current && payload.session_id !== sessionIdRef.current) return;
      handlersRef.current.onHistory?.(payload);
    };
    const handleExit = (payload: { session_id: string; exit_code: number }) => {
      if (sessionIdRef.current && payload.session_id !== sessionIdRef.current) return;
      setConnected(false);
      handlersRef.current.onExit?.(payload);
    };
    const handleError = (payload: { session_id?: string; code?: string; message: string }) => {
      if (payload.session_id && sessionIdRef.current && payload.session_id !== sessionIdRef.current) return;
      handlersRef.current.onError?.(payload);
    };

    wsClient.on('terminal_opened', handleOpened);
    wsClient.on('terminal_history', handleHistory);
    wsClient.on('terminal_output', handleOutput);
    wsClient.on('terminal_exit', handleExit);
    wsClient.on('terminal_error', handleError);

    return () => {
      wsClient.off('terminal_opened', handleOpened);
      wsClient.off('terminal_history', handleHistory);
      wsClient.off('terminal_output', handleOutput);
      wsClient.off('terminal_exit', handleExit);
      wsClient.off('terminal_error', handleError);
      openedRef.current = false;
      const sessionId = sessionIdRef.current;
      sessionIdRef.current = null;
      setConnected(false);
      if (sessionId) {
        wsClient.closeTerminal(sessionId);
      }
    };
  }, [targetId, backend]);

  return {
    connected,
    open(cols: number, rows: number) {
      if (openedRef.current) return;
      openedRef.current = true;
      wsClient.openTerminal(targetId, backend, cols, rows);
    },
    sendInput(data: string) {
      const sessionId = sessionIdRef.current;
      if (!sessionId) return;
      wsClient.sendTerminalInput(sessionId, data);
    },
    scroll(lines: number) {
      const sessionId = sessionIdRef.current;
      if (!sessionId) return;
      wsClient.scrollTerminal(sessionId, lines);
    },
    resize(cols: number, rows: number) {
      const sessionId = sessionIdRef.current;
      if (!sessionId) {
        if (!openedRef.current) {
          openedRef.current = true;
          wsClient.openTerminal(targetId, backend, cols, rows);
        }
        return;
      }
      wsClient.resizeTerminal(sessionId, cols, rows);
    },
  };
}
