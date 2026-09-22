import React, { useEffect, useRef, useState } from "react";
import { Alert, Pressable, Text, View } from "react-native";
import * as Clipboard from "expo-clipboard";
import { Ionicons } from "@expo/vector-icons";
import { useCurrentServer } from "../store/currentServer";
import { useAppColors } from "../constants/tokens";
import type { DiscoveredSessionService } from "../services/sessionServicesPresentation";
import type { ServiceTunnel } from "../services/sessionServices";
import { wsClient } from "../services/websocket";

export function ServiceTunnelControls({ service, onOpenURL }: { service: DiscoveredSessionService; onOpenURL(url: string): void }) {
  const colors = useAppColors();
  const { currentServerId } = useCurrentServer();
  const scope = JSON.stringify([currentServerId, service.serverId, service.id, service.generation]);
  const currentScope = useRef(scope); currentScope.current = scope;
  const [state, setState] = useState<{ scope: string; tunnel?: ServiceTunnel; error?: string; busy?: boolean }>({ scope, tunnel: service.tunnel });
  const current = state.scope === scope ? state : { scope, tunnel: service.tunnel };
  const tunnel = current.tunnel;
  const active = tunnel?.status === "running" || tunnel?.status === "starting" || tunnel?.status === "stopping";
  async function action(kind: "start" | "stop" | "status") {
    if (!service.generation || currentServerId !== service.serverId) return;
    if (kind !== "status") setState((value) => ({ ...value, scope, busy: true, error: undefined }));
    try {
      const next = await wsClient.serviceTunnel(service.serverId, service.id, service.generation, kind);
      if (currentScope.current === scope) setState({ scope, tunnel: next });
    } catch (error) {
      if (currentScope.current === scope) setState((value) => ({ ...value, scope, busy: false, error: error instanceof Error ? error.message : "Tunnel unavailable" }));
    }
  }
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      if (cancelled) return;
      await action("status");
      if (!cancelled) timer = setTimeout(poll, 2000);
    }
    if (active) timer = setTimeout(poll, 2000);
    return () => { cancelled = true; clearTimeout(timer); };
    // Status polling belongs to this exact server/service generation.
  }, [scope, active]);
  useEffect(() => { currentScope.current = scope; return () => { if (currentScope.current === scope) currentScope.current = ""; }; }, [scope]);
  if (!service.generation || currentServerId !== service.serverId) return null;
  const publicURL = tunnel?.status === "running" ? tunnel.url : undefined;
  return <View style={{ gap: 6, marginTop: 8 }}>
    {publicURL ? <Text selectable numberOfLines={2} style={{ color: colors.textSecondary }}>{publicURL}</Text> : null}
    <View style={{ flexDirection: "row", gap: 16, alignItems: "center", flexWrap: "wrap" }}>
      <Pressable accessibilityRole="button" disabled={current.busy} accessibilityLabel={active ? "Stop public tunnel" : "Start temporary public tunnel"}
        onPress={() => active ? void action("stop") : Alert.alert("Share this service publicly?", "Anyone with the temporary URL can access this service. Quick Tunnels do not support SSE and allow 200 in-flight requests.", [{ text: "Cancel", style: "cancel" }, { text: "Start tunnel", onPress: () => void action("start") }])}
        style={{ minHeight: 44, flexDirection: "row", gap: 6, alignItems: "center" }}>
        <Ionicons name={active ? "stop-circle-outline" : "globe-outline"} size={17} color={colors.textSecondary} />
        <Text style={{ color: colors.textPrimary }}>{current.busy ? "Working…" : active ? "Stop tunnel" : "Quick Tunnel"}</Text>
      </Pressable>
      {publicURL ? <>
        <Pressable accessibilityRole="button" accessibilityLabel="Open public URL" onPress={() => onOpenURL(publicURL)} style={{ paddingVertical: 12 }}><Text style={{ color: colors.textPrimary }}>Open</Text></Pressable>
        <Pressable accessibilityRole="button" accessibilityLabel="Copy public URL" onPress={() => void Clipboard.setStringAsync(publicURL)} style={{ paddingVertical: 12 }}><Text style={{ color: colors.textPrimary }}>Copy</Text></Pressable>
      </> : active ? <Text style={{ color: colors.textSecondary }}>{tunnel?.status}</Text> : null}
    </View>
    {current.error || tunnel?.error ? <Text style={{ color: colors.textSecondary }}>{current.error || tunnel?.error}</Text> : null}
  </View>;
}
