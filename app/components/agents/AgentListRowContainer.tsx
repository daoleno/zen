import React, { memo, useCallback, useMemo } from 'react';
import * as Haptics from 'expo-haptics';
import type { Agent } from '../../store/agents';
import { presentAgent } from '../../services/agentPresentation';
import { formatAgentSessionPreview } from '../../services/sessionPreview';
import { isAgentActivelyRunning } from '../../services/agentStatusPresentation';
import { formatTelegramListTime } from '../../constants/telegramPresentation';
import { shortAgentLabel } from '../../services/sessionServicesPresentation';
import { AgentSessionRow } from './AgentSessionRow';

interface AgentListRowContainerProps {
  agent: Agent;
  alias?: string;
  linkedWorkTitle?: string;
  onOpenAgent(agent: Agent): void;
  onOpenContextMenu(agent: Agent): void;
  showServerName: boolean;
}

function AgentListRowContainerComponent({
  agent,
  alias,
  linkedWorkTitle,
  onOpenAgent,
  onOpenContextMenu,
  showServerName,
}: AgentListRowContainerProps) {
  const rowModel = useMemo(() => {
    const presented = presentAgent(agent, alias);
    const workTitle = linkedWorkTitle?.trim();
    const title =
      presented.titleSource !== 'default'
        ? presented.title
        : workTitle ||
          presented.shortTitle ||
          shortAgentLabel(agent.name) ||
          presented.title;
    const preview = formatAgentSessionPreview(agent, {
      showServerName,
      serverName: agent.serverName,
    });
    return { presented, preview, title };
  }, [agent, alias, linkedWorkTitle, showServerName]);

  const handlePress = useCallback(() => {
    onOpenAgent(agent);
  }, [agent, onOpenAgent]);
  const handleLongPress = useCallback(() => {
    void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    onOpenContextMenu(agent);
  }, [agent, onOpenContextMenu]);

  return (
    <AgentSessionRow
      title={rowModel.title}
      kind={rowModel.presented.kind}
      terminalFlavor={rowModel.presented.terminalFlavor}
      preview={rowModel.preview.text}
      previewTone={rowModel.preview.tone}
      previewPrefix={rowModel.preview.prefix}
      timeLabel={
        isAgentActivelyRunning(agent.status)
          ? 'live'
          : formatTelegramListTime(agent.updated_at)
      }
      status={agent.status}
      showBrainBadge={Boolean(agent.delegated)}
      onPress={handlePress}
      onLongPress={handleLongPress}
    />
  );
}

export const AgentListRowContainer = memo(AgentListRowContainerComponent);
