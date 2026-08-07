import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocalSearchParams, useRouter } from "expo-router";
import { makeSessionKey } from "../../../services/sessionKeys";
import type { StoredInterfaceRenderMode } from "../../../services/storage";
import type { TerminalSurfaceHandle } from "../TerminalSurface";
import {
  consumeInterfaceComposerInitialFocusGrant,
  isInterfaceComposerInitialFocusRouteGrant,
  reconcileInterfaceComposerInitialFocusGrant,
  type InterfaceComposerInitialFocusGrant,
} from "../interfaceComposerInitialFocus";

export interface TerminalRouteSessionHint {
  name?: string;
  cwd?: string;
  command?: string;
  startedAt?: number;
}

export function useTerminalScreenLocalState() {
  const router = useRouter();
  const params = useLocalSearchParams<{
    id?: string;
    serverId?: string;
    name?: string;
    cwd?: string;
    command?: string;
    startedAt?: string;
    initialInterfaceRenderMode?: string;
    initialComposerFocus?: string;
    skillsHandoff?: string;
    createDurabilityWarning?: string;
  }>();
  const agentId = paramString(params.id);
  const serverId = paramString(params.serverId);
  const initialInterfaceRenderMode = paramInterfaceRenderMode(
    params.initialInterfaceRenderMode,
  );
  const sessionKey =
    agentId && serverId ? makeSessionKey(serverId, agentId) : null;
  const routeSkillsHandoffToken = paramRawString(params.skillsHandoff);
  const initialComposerFocusRequested =
    isInterfaceComposerInitialFocusRouteGrant(
      paramRawString(params.initialComposerFocus),
    );
  const routeSessionHint = useMemo<TerminalRouteSessionHint>(
    () => ({
      name: paramString(params.name) || undefined,
      cwd: paramString(params.cwd) || undefined,
      command: paramString(params.command) || undefined,
      startedAt: paramTimestamp(params.startedAt),
    }),
    [params.command, params.cwd, params.name, params.startedAt],
  );
  const [renameVisible, setRenameVisible] = useState(false);
  const [renameDraft, setRenameDraft] = useState("");
  const [newTerminalVisible, setNewTerminalVisible] = useState(false);
  const [creatingSession, setCreatingSession] = useState(false);
  const [screenFocused, setScreenFocused] = useState(false);
  const [initialComposerFocusGrant, setInitialComposerFocusGrant] =
    useState<InterfaceComposerInitialFocusGrant>(null);
  const [skillsHandoffGrant, setSkillsHandoffGrant] = useState<{
    sessionKey: string;
    token: string;
  } | null>(() =>
    sessionKey && routeSkillsHandoffToken
      ? { sessionKey, token: routeSkillsHandoffToken }
      : null,
  );
  const terminalRef = useRef<TerminalSurfaceHandle>(null);

  useEffect(() => {
    setInitialComposerFocusGrant((current) =>
      reconcileInterfaceComposerInitialFocusGrant(current, {
        sessionKey,
        requested: initialComposerFocusRequested,
      }),
    );
    if (initialComposerFocusRequested) {
      router.setParams({ initialComposerFocus: "" });
    }
  }, [initialComposerFocusRequested, router, sessionKey]);

  useEffect(() => {
    if (sessionKey && routeSkillsHandoffToken) {
      setSkillsHandoffGrant({
        sessionKey,
        token: routeSkillsHandoffToken,
      });
      router.setParams({ skillsHandoff: "" });
    }
  }, [routeSkillsHandoffToken, router, sessionKey]);

  const createDurabilityWarningParam = paramString(params.createDurabilityWarning);
  const [createDurabilityWarning, setCreateDurabilityWarning] = useState<
    string | null
  >(null);
  useEffect(() => {
    if (!createDurabilityWarningParam) return;
    setCreateDurabilityWarning(createDurabilityWarningParam);
    router.setParams({ createDurabilityWarning: "" });
  }, [createDurabilityWarningParam, router]);

  const skillsHandoffToken =
    skillsHandoffGrant?.sessionKey === sessionKey
      ? skillsHandoffGrant.token
      : undefined;

  const consumeInitialComposerFocus = useCallback(() => {
    router.setParams({ initialComposerFocus: "" });
    setInitialComposerFocusGrant((current) =>
      consumeInterfaceComposerInitialFocusGrant(current, sessionKey),
    );
  }, [router, sessionKey]);

  useEffect(() => {
    setRenameVisible(false);
    setRenameDraft("");
  }, [sessionKey]);

  return {
    agentId,
    serverId,
    sessionKey,
    initialComposerFocusGrant,
    consumeInitialComposerFocus,
    initialInterfaceRenderMode,
    routeSessionHint,
    renameVisible,
    setRenameVisible,
    renameDraft,
    setRenameDraft,
    newTerminalVisible,
    setNewTerminalVisible,
    creatingSession,
    setCreatingSession,
    createDurabilityWarning,
    dismissCreateDurabilityWarning: () => setCreateDurabilityWarning(null),
    screenFocused,
    setScreenFocused,
    terminalRef,
    skillsHandoffToken,
  };
}

function paramRawString(value: string | string[] | undefined): string {
  return Array.isArray(value) ? value[0] || "" : value || "";
}

function paramString(value: string | string[] | undefined): string {
  if (Array.isArray(value)) {
    return value[0]?.trim() || "";
  }
  return value?.trim() || "";
}

function paramTimestamp(
  value: string | string[] | undefined,
): number | undefined {
  const raw = paramString(value);
  if (!raw) return undefined;
  const numeric = Number(raw);
  return Number.isFinite(numeric) && numeric > 0 ? numeric : undefined;
}

function paramInterfaceRenderMode(
  value: string | string[] | undefined,
): StoredInterfaceRenderMode | undefined {
  const raw = paramString(value);
  return raw === "chat" || raw === "terminal" ? raw : undefined;
}
