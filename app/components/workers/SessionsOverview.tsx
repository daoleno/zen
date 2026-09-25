import React, { memo } from "react";
import { StyleSheet, View } from "react-native";
import type { ConnectionIssue } from "../../services/connectionIssue";
import { InlineNotice } from "../ui/InlineNotice";

interface SessionsOverviewProps {
  serverName: string | null;
  connection: "connected" | "connecting" | "offline";
  issue: ConnectionIssue | null;
  /** Work items on the current server that are blocked on the user. */
  attentionCount: number;
  onOpenWorkActivity(): void;
  onRetry(): void;
}

/**
 * The only chrome above the Sessions list. It stays empty in the normal case
 * and speaks only when something needs the user: the current server is
 * unreachable, or Work is waiting on them. Creation, Services and the full
 * Work list live in the page action menu and the floating create button.
 */
function SessionsOverviewComponent({
  serverName,
  connection,
  issue,
  attentionCount,
  onOpenWorkActivity,
  onRetry,
}: SessionsOverviewProps) {
  const showOfflineNotice = Boolean(serverName) && connection === "offline";
  const showAttention = attentionCount > 0;
  if (!showOfflineNotice && !showAttention) {
    return <View style={styles.spacer} />;
  }
  return (
    <View style={styles.root}>
      {showOfflineNotice ? (
        <InlineNotice
          tone={issue ? "danger" : "warning"}
          title={issue?.title ?? `${serverName} is offline`}
          detail={issue?.detail ?? "Sessions below may be out of date."}
          action={{ label: "Retry", onPress: onRetry }}
        />
      ) : null}
      {showAttention ? (
        <InlineNotice
          tone="warning"
          icon="hand-left-outline"
          title={attentionCount === 1 ? "1 Work item needs you" : `${attentionCount} Work items need you`}
          action={{ label: "Review", onPress: onOpenWorkActivity }}
        />
      ) : null}
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
