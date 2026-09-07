import React from "react";
import { Alert } from "react-native";
import { useLocalSearchParams, useRouter } from "expo-router";
import { useCurrentServer } from "../store/currentServer";
import { useWorkerServerSummary } from "../store/workers";
import { wsClient } from "../services/websocket";
import { setServerAutoConnect } from "../services/storage";
import { OnboardingPresentation } from "../components/onboarding/OnboardingPresentation";

export default function OnboardingScreen() {
  const router = useRouter();
  const { paired } = useLocalSearchParams<{ paired?: string }>();
  const { currentServer, isCurrentServer } = useCurrentServer();
  const { serverConnections, serverConnectionIssues } = useWorkerServerSummary();
  const server = paired === "1" ? currentServer : null;
  return (
    <OnboardingPresentation
      serverName={server?.name}
      connection={server ? serverConnections[server.id] ?? "connecting" : undefined}
      error={server ? serverConnectionIssues[server.id]?.detail : undefined}
      onPair={(mode) => router.push({ pathname: "/settings", params: {
        addServer: Date.now().toString(), pairingRequired: "1", pairMode: mode,
      } })}
      onContinue={() => router.replace("/")}
      onSettings={() => router.push("/settings")}
      onRetry={() => {
        if (!server || !isCurrentServer(server.id)) return;
        void setServerAutoConnect(server.id, true).then(() => {
          if (isCurrentServer(server.id)) wsClient.connectServer(server);
        }).catch((error) => Alert.alert("Connection failed", error instanceof Error ? error.message : "Could not retry this server."));
      }}
    />
  );
}
