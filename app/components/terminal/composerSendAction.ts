export function resolveComposerSendAction({
  canSend,
  connected,
  elapsedStartedAt,
  hasComposerContent,
  interrupting,
  activityRunning,
}: {
  canSend: boolean;
  connected: boolean;
  elapsedStartedAt?: string;
  hasComposerContent: boolean;
  interrupting: boolean;
  activityRunning: boolean;
}) {
  const showStopButton = activityRunning && !hasComposerContent;
  return {
    showStopButton,
    providerActivityStartedAt: activityRunning ? elapsedStartedAt : undefined,
    stopEnabled: connected && activityRunning && !interrupting,
    stopLabel: interrupting ? "Stopping response" : "Stop response",
    sendEnabled: canSend,
    sendLabel: "Send message",
  };
}
