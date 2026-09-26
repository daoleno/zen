import React, { memo } from "react";
import { StyleSheet, View } from "react-native";
import type { ConnectionIssue } from "../../services/connectionIssue";
import { InlineNotice } from "../ui/InlineNotice";

interface SessionsOverviewProps {
  serverName: string | null;
  connection: "connected" | "connecting" | "offline";
  issue: ConnectionIssue | null;
  onRetry(): void;
}

/**
 * The only chrome above the Sessions list. It stays empty in the normal case
 * and speaks only when the current server is unreachable. New session and
 * Services live in the page action menu; the floating button creates a Session.
 */
function SessionsOverviewComponent({
  serverName,
  connection,
  issue,
  onRetry,
}: SessionsOverviewProps) {
  const showOfflineNotice = Boolean(serverName) && connection === "offline";
  if (!showOfflineNotice) {
    return <View style={styles.spacer} />;
  }
  return (
    <View style={styles.root}>
      <InlineNotice
        tone={issue ? "danger" : "warning"}
        title={issue?.title ?? `${serverName} is offline`}
        detail={issue?.detail ?? "Sessions below may be out of date."}
        action={{ label: "Retry", onPress: onRetry }}
      />
    </View>
  );
}

export const SessionsOverview = memo(SessionsOverviewComponent);

const styles = StyleSheet.create({
  root: {
    paddingHorizontal: 16,
    paddingTop: 8,
    paddingBottom: 8,
    gap: 10,
  },
  spacer: {
    height: 8,
  },
});
