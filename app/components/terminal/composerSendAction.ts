export function resolveComposerSendAction({
  canSend,
  connected,
  elapsedStartedAt,
  hasComposerContent,
  interrupting,
  requestRunning,
  startingNewChat,
}: {
  canSend: boolean;
  connected: boolean;
  elapsedStartedAt?: string;
  hasComposerContent: boolean;
  interrupting: boolean;
  requestRunning: boolean;
  sending: boolean;
  startingNewChat: boolean;
}) {
  const showStopButton = requestRunning && !hasComposerContent;
  return {
    primaryAction: showStopButton ? "stop" as const : "send" as const,
    showStopButton,
    showStopIndicator: showStopButton,
    workingTurnStartedAt: requestRunning ? elapsedStartedAt : undefined,
    stopEnabled: connected && requestRunning && !interrupting,
    stopLabel: interrupting ? "Stopping response" : "Stop response",
    sendEnabled: canSend || startingNewChat,
    sendLabel: startingNewChat ? "Starting new chat" : "Send message",
  };
}
