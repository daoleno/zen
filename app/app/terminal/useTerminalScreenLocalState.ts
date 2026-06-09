import { useEffect, useMemo, useRef, useState } from "react";
import { useLocalSearchParams } from "expo-router";
import { makeSessionKey } from "../../services/sessionKeys";
import type { StoredCodexRenderMode } from "../../services/storage";
import type { TerminalSurfaceHandle } from "../../components/terminal/TerminalSurface";

export interface TerminalRouteSessionHint {
  name?: string;
  cwd?: string;
  command?: string;
  startedAt?: number;
}

export function useTerminalScreenLocalState() {
  const params = useLocalSearchParams<{
    id?: string;
    serverId?: string;
    name?: string;
    cwd?: string;
    command?: string;
    startedAt?: string;
    initialCodexRenderMode?: string;
  }>();
  const agentId = paramString(params.id);
  const serverId = paramString(params.serverId);
  const initialCodexRenderMode = paramCodexRenderMode(
    params.initialCodexRenderMode,
  );
  const sessionKey =
    agentId && serverId ? makeSessionKey(serverId, agentId) : null;
  const routeSessionHint = useMemo<TerminalRouteSessionHint>(
    () => ({
      name: paramString(params.name) || undefined,
      cwd: paramString(params.cwd) || undefined,
      command: paramString(params.command) || undefined,
      startedAt: paramTimestamp(params.startedAt),
    }),
    [params.command, params.cwd, params.name, params.startedAt],
  );
  const [pickerVisible, setPickerVisible] = useState(false);
  const [renameVisible, setRenameVisible] = useState(false);
  const [renameDraft, setRenameDraft] = useState("");
  const [newTerminalVisible, setNewTerminalVisible] = useState(false);
  const [creatingSession, setCreatingSession] = useState(false);
  const [screenFocused, setScreenFocused] = useState(false);
  const terminalRef = useRef<TerminalSurfaceHandle>(null);

  useEffect(() => {
    setRenameVisible(false);
    setRenameDraft("");
  }, [sessionKey]);

  return {
    agentId,
    serverId,
    sessionKey,
    initialCodexRenderMode,
    routeSessionHint,
    pickerVisible,
    setPickerVisible,
    renameVisible,
    setRenameVisible,
    renameDraft,
    setRenameDraft,
    newTerminalVisible,
    setNewTerminalVisible,
    creatingSession,
    setCreatingSession,
    screenFocused,
    setScreenFocused,
    terminalRef,
  };
}

function paramString(value: string | string[] | undefined): string {
  if (Array.isArray(value)) {
    return value[0]?.trim() || "";
  }
  return value?.trim() || "";
}

function paramTimestamp(value: string | string[] | undefined): number | undefined {
  const raw = paramString(value);
  if (!raw) return undefined;
  const numeric = Number(raw);
  return Number.isFinite(numeric) && numeric > 0 ? numeric : undefined;
}

function paramCodexRenderMode(
  value: string | string[] | undefined,
): StoredCodexRenderMode | undefined {
  const raw = paramString(value);
  return raw === "chat" || raw === "terminal" ? raw : undefined;
}
