export function resolveComposerSendAction({
  canSend,
  connected,
  hasComposerContent,
  interrupting,
  requestRunning,
  sending,
  startingNewChat,
}: {
  canSend: boolean;
  connected: boolean;
  hasComposerContent: boolean;
  interrupting: boolean;
  requestRunning: boolean;
  sending: boolean;
  startingNewChat: boolean;
}) {
  const showStopIndicator =
    connected && requestRunning && !hasComposerContent;
  const showStopButton = showStopIndicator && !sending;
  return {
    showStopButton,
    showStopIndicator,
    sendEnabled: canSend || showStopButton || startingNewChat,
    sendLabel: startingNewChat
      ? "Starting new chat"
      : showStopButton
        ? "Stop response"
        : showStopIndicator && interrupting
          ? "Stopping"
          : showStopIndicator
            ? "Working"
            : "Send message",
  };
}
