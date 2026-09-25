import { useEffect, useRef, useState } from "react";
import { Alert } from "react-native";
import { useCurrentServer } from "../../store/currentServer";
import type { DiscoveredSessionService } from "../../services/sessionServicesPresentation";
import type { ServiceTunnel } from "../../services/sessionServices";
import { wsClient } from "../../services/websocket";

export interface ServiceTunnelState {
  /** False when the service has no generation or belongs to another server. */
  available: boolean;
  tunnel?: ServiceTunnel;
  /** starting, running or stopping. */
  active: boolean;
  publicURL?: string;
  busy: boolean;
  error?: string;
  /** Asks for confirmation before exposing the service publicly. */
  start(): void;
  stop(): void;
}

/**
 * Quick Tunnel state for one service generation on the current server. Status
 * polling belongs to that exact server/service/generation and stops as soon
 * as the tunnel settles, the scope changes or the owner unmounts.
 */
export function useServiceTunnel(service: DiscoveredSessionService): ServiceTunnelState {
  const { currentServerId } = useCurrentServer();
  const scope = JSON.stringify([currentServerId, service.serverId, service.id, service.generation]);
  const currentScope = useRef(scope);
  currentScope.current = scope;
  const [state, setState] = useState<{ scope: string; tunnel?: ServiceTunnel; error?: string; busy?: boolean }>({
    scope,
    tunnel: service.tunnel,
  });
  const current = state.scope === scope ? state : { scope, tunnel: service.tunnel };
  const tunnel = current.tunnel;
  const active = tunnel?.status === "running" || tunnel?.status === "starting" || tunnel?.status === "stopping";
  const available = Boolean(service.generation) && currentServerId === service.serverId;

  async function run(kind: "start" | "stop" | "status") {
    if (!service.generation || currentServerId !== service.serverId) return;
    if (kind !== "status") setState((value) => ({ ...value, scope, busy: true, error: undefined }));
    try {
      const next = await wsClient.serviceTunnel(service.serverId, service.id, service.generation, kind);
      if (currentScope.current === scope) setState({ scope, tunnel: next });
    } catch (error) {
      if (currentScope.current === scope) {
        setState((value) => ({
          ...value,
          scope,
          busy: false,
          error: error instanceof Error ? error.message : "Tunnel unavailable",
        }));
      }
    }
  }

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      if (cancelled) return;
      await run("status");
      if (!cancelled) timer = setTimeout(poll, 2000);
    }
    if (active) timer = setTimeout(poll, 2000);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
    // Status polling belongs to this exact server/service generation.
  }, [scope, active]);
  useEffect(() => {
    currentScope.current = scope;
    return () => {
      if (currentScope.current === scope) currentScope.current = "";
    };
  }, [scope]);

  return {
    available,
    tunnel,
    active,
    publicURL: tunnel?.status === "running" ? tunnel.url : undefined,
    busy: Boolean(current.busy),
    error: current.error || tunnel?.error,
    start: () =>
      Alert.alert(
        "Share this service publicly?",
        "Anyone with the temporary URL can access this service. Quick Tunnels do not support SSE and allow 200 in-flight requests.",
        [
          { text: "Cancel", style: "cancel" },
          { text: "Start tunnel", onPress: () => void run("start") },
        ],
      ),
    stop: () => void run("stop"),
  };
}
