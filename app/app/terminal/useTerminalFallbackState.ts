import { useEffect, useRef, useState } from "react";
import type { ConnectionState } from "../../store/agents";
import type { ConnectionIssue } from "../../services/connectionIssue";

const RECONNECT_FALLBACK_DELAY_MS = 1500;

interface UseTerminalFallbackStateInput {
  hasTerminalRoute: boolean;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
}

export function useTerminalFallbackState({
  hasTerminalRoute,
  connectionState,
  connectionIssue,
}: UseTerminalFallbackStateInput) {
  const [showTerminalFallback, setShowTerminalFallback] = useState(
    !hasTerminalRoute,
  );
  const reconnectFallbackTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );

  useEffect(() => {
    if (reconnectFallbackTimerRef.current) {
      clearTimeout(reconnectFallbackTimerRef.current);
      reconnectFallbackTimerRef.current = null;
    }

    if (!hasTerminalRoute) {
      setShowTerminalFallback(true);
      return;
    }

    if (connectionState === "connected") {
      setShowTerminalFallback(false);
      return;
    }

    if (connectionIssue) {
      setShowTerminalFallback(true);
      return;
    }

    setShowTerminalFallback(false);
    reconnectFallbackTimerRef.current = setTimeout(() => {
      setShowTerminalFallback(true);
      reconnectFallbackTimerRef.current = null;
    }, RECONNECT_FALLBACK_DELAY_MS);

    return () => {
      if (reconnectFallbackTimerRef.current) {
        clearTimeout(reconnectFallbackTimerRef.current);
        reconnectFallbackTimerRef.current = null;
      }
    };
  }, [connectionIssue, connectionState, hasTerminalRoute]);

  return showTerminalFallback;
}
