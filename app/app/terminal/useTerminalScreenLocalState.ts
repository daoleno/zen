import { useEffect, useRef, useState } from "react";
import { useLocalSearchParams } from "expo-router";
import { makeSessionKey } from "../../services/sessionKeys";
import type { TerminalSurfaceHandle } from "../../components/terminal/TerminalSurface";

export function useTerminalScreenLocalState() {
  const params = useLocalSearchParams<{ id?: string; serverId?: string }>();
  const agentId = typeof params.id === "string" ? params.id : "";
  const serverId = typeof params.serverId === "string" ? params.serverId : "";
  const sessionKey =
    agentId && serverId ? makeSessionKey(serverId, agentId) : null;
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
