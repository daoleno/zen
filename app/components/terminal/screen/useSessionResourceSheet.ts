import { useCallback, useEffect, useRef, useState } from "react";
import {
  acceptSessionResourceSnapshotResponse,
  type SessionResourceSnapshot,
} from "../../../services/sessionResourceSnapshot";
import { wsClient } from "../../../services/websocket";

interface UseSessionResourceSheetInput {
  serverId: string;
  agentId: string;
  connectionConnected: boolean;
}

export function useSessionResourceSheet({
  serverId,
  agentId,
  connectionConnected,
}: UseSessionResourceSheetInput) {
  const [visible, setVisible] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [snapshot, setSnapshot] = useState<SessionResourceSnapshot | null>(null);
  const requestSeqRef = useRef(0);

  const clearProjection = useCallback(() => {
    requestSeqRef.current += 1;
    setSnapshot(null);
    setError(null);
    setLoading(false);
  }, []);

  const close = useCallback(() => {
    setVisible(false);
    clearProjection();
  }, [clearProjection]);

  const open = useCallback(() => {
    setVisible(true);
  }, []);

  const load = useCallback(async () => {
    if (!visible || !serverId || !agentId) {
      return;
    }
    if (!connectionConnected) {
      setLoading(false);
      setSnapshot(null);
      setError("Reconnect to the daemon before loading resource usage.");
      return;
    }

    const requestSeq = requestSeqRef.current + 1;
    requestSeqRef.current = requestSeq;
    setLoading(true);
    setError(null);

    try {
      const next = await wsClient.getSessionResourceSnapshot(serverId, agentId);
      if (
        !acceptSessionResourceSnapshotResponse({
          requestSeq,
          currentSeq: requestSeqRef.current,
          snapshotAgentId: next.agent_id,
          expectedAgentId: agentId,
        })
      ) {
        return;
      }
      setSnapshot(next);
      setError(null);
    } catch (err) {
      if (requestSeqRef.current !== requestSeq) {
        return;
      }
      setSnapshot(null);
      setError(err instanceof Error ? err.message : "Resource usage failed.");
    } finally {
      if (requestSeqRef.current === requestSeq) {
        setLoading(false);
      }
    }
  }, [agentId, connectionConnected, serverId, visible]);

  useEffect(() => {
    // Session or server identity changes must never resurrect a prior projection.
    clearProjection();
    setVisible(false);
  }, [agentId, serverId, clearProjection]);

  useEffect(() => {
    if (!visible) {
      return;
    }
    void load();
  }, [visible, load]);

  useEffect(() => {
    if (!visible || connectionConnected) {
      return;
    }
    requestSeqRef.current += 1;
    setSnapshot(null);
    setLoading(false);
    setError("Reconnect to the daemon before loading resource usage.");
  }, [connectionConnected, visible]);

  return {
    visible,
    loading,
    error,
    snapshot,
    open,
    close,
    retry: () => {
      void load();
    },
  };
}
