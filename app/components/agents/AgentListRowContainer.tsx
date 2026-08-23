import React, { memo, useCallback, useMemo } from 'react';
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
  /** Normal mode long press: enter selection mode with this Session selected. */
  onEnterSelection(agent: Agent): void;
  /** Selection mode tap: toggle this Session's selection. */
  onToggleSelection(agent: Agent): void;
  selectionMode: boolean;
  selected: boolean;
  /** Row excluded from termination (daemon offline): disabled inside selection. */
  selectionDisabled: boolean;
  showServerName: boolean;
}

function AgentListRowContainerComponent({
  agent,
  alias,
  linkedWorkTitle,
  onOpenAgent,
  onEnterSelection,
  onToggleSelection,
  selectionMode,
  selected,
  selectionDisabled,
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
    if (selectionMode) {
      if (!selectionDisabled) {
        onToggleSelection(agent);
      }
      return;
    }
    onOpenAgent(agent);
  }, [agent, onOpenAgent, onToggleSelection, selectionDisabled, selectionMode]);

  const handleLongPress = useCallback(() => {
    if (selectionMode) {
      return;
    }
    onEnterSelection(agent);
  }, [agent, onEnterSelection, selectionMode]);

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
      brainDelegated={Boolean(agent.delegated)}
      onPress={handlePress}
      onLongPress={handleLongPress}
      selectionMode={selectionMode}
      selected={selected}
      selectionDisabled={selectionDisabled}
      onToggleSelection={handlePress}
    />
  );
}

export const AgentListRowContainer = memo(AgentListRowContainerComponent);
