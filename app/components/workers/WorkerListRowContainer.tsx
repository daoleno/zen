import React, { memo, useCallback, useMemo } from 'react';
import type { Worker } from '../../store/workers';
import { presentWorker } from '../../services/workerPresentation';
import { formatWorkerSessionPreview } from '../../services/sessionPreview';
import { isWorkerActivelyRunning } from '../../services/workerStatusPresentation';
import { formatTelegramListTime } from '../../constants/telegramPresentation';
import { shortWorkerLabel } from '../../services/sessionServicesPresentation';
import { WorkerSessionRow } from './WorkerSessionRow';

interface WorkerListRowContainerProps {
  agent: Worker;
  alias?: string;
  linkedWorkTitle?: string;
  onOpenWorker(agent: Worker): void;
  /** Normal mode long press: enter selection mode with this Session selected. */
  onEnterSelection(agent: Worker): void;
  /** Selection mode tap: toggle this Session's selection. */
  onToggleSelection(agent: Worker): void;
  selectionMode: boolean;
  selected: boolean;
  /** Row excluded from termination (daemon offline): disabled inside selection. */
  selectionDisabled: boolean;
  showServerName: boolean;
}

function WorkerListRowContainerComponent({
  agent,
  alias,
  linkedWorkTitle,
  onOpenWorker,
  onEnterSelection,
  onToggleSelection,
  selectionMode,
  selected,
  selectionDisabled,
  showServerName,
}: WorkerListRowContainerProps) {
  const rowModel = useMemo(() => {
    const presented = presentWorker(agent, alias);
    const workTitle = linkedWorkTitle?.trim();
    const title =
      presented.titleSource !== 'default'
        ? presented.title
        : workTitle ||
          presented.shortTitle ||
          shortWorkerLabel(agent.name) ||
          presented.title;
    const preview = formatWorkerSessionPreview(agent, {
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
    onOpenWorker(agent);
  }, [agent, onOpenWorker, onToggleSelection, selectionDisabled, selectionMode]);

  const handleLongPress = useCallback(() => {
    if (selectionMode) {
      return;
    }
    onEnterSelection(agent);
  }, [agent, onEnterSelection, selectionMode]);

  return (
    <WorkerSessionRow
      title={rowModel.title}
      kind={rowModel.presented.kind}
      terminalFlavor={rowModel.presented.terminalFlavor}
      preview={rowModel.preview.text}
      previewTone={rowModel.preview.tone}
      previewPrefix={rowModel.preview.prefix}
      timeLabel={
        isWorkerActivelyRunning(agent.status)
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

export const WorkerListRowContainer = memo(WorkerListRowContainerComponent);
