import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ConnectionState } from "../../store/workers";
import {
  buildGitDiffChipLabel,
  type GitDiffPageRequest,
  type GitDiffStatusSnapshot,
  type GitRepoBrowserEntry,
  type GitRepoFileContentPayload,
} from "../../services/gitDiff";
import { wsClient } from "../../services/websocket";

interface UseTerminalGitDiffInput {
  serverId: string;
  workerId: string;
  cwd: string;
  connectionState: ConnectionState;
  hasTerminalRoute: boolean;
  screenFocused: boolean;
}

export type TerminalGitDiffTone = "clean" | "dirty" | "error" | "loading";
export interface TerminalGitDiffSummary {
  label: string;
  tone: TerminalGitDiffTone;
  additions: number;
  deletions: number;
  fileCount: number;
  showStats: boolean;
}

export function useTerminalGitDiff({
  serverId,
  workerId,
  cwd,
  connectionState,
  hasTerminalRoute,
  screenFocused,
}: UseTerminalGitDiffInput) {
  const ownerKey = JSON.stringify([serverId, workerId, cwd]);
  const [visible, setVisible] = useState(false);
  const [result, setResult] = useState<{
    owner: string;
    status: GitDiffStatusSnapshot;
  } | null>(null);
  const status = result?.owner === ownerKey ? result.status : null;
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const epoch = useRef(0);
  const statusRequest = useRef(0);
  const browserRequest = useRef(0);
  const fileRequest = useRef(0);
  const [repoBrowserPath, setRepoBrowserPath] = useState("");
  const [repoBrowserEntries, setRepoBrowserEntries] = useState<
    GitRepoBrowserEntry[]
  >([]);
  const [repoBrowserLoading, setRepoBrowserLoading] = useState(false);
  const [repoBrowserError, setRepoBrowserError] = useState<string | null>(null);
  const [repoFilePath, setRepoFilePath] = useState<string | null>(null);
  const [repoFileLoadingPath, setRepoFileLoadingPath] = useState<string | null>(
    null,
  );
  const [repoFileError, setRepoFileError] = useState<string | null>(null);
  const [repoFileByPath, setRepoFileByPath] = useState<
    Record<string, GitRepoFileContentPayload | undefined>
  >({});
  const queryEnabled = Boolean(
    hasTerminalRoute && screenFocused && serverId && workerId && cwd,
  );
  const connected = queryEnabled && connectionState === "connected";

  useEffect(() => {
    epoch.current++;
    setVisible(false);
    setResult(null);
    setError(null);
    setRepoBrowserPath("");
    setRepoBrowserEntries([]);
    setRepoBrowserError(null);
    setRepoBrowserLoading(false);
    setRepoFilePath(null);
    setRepoFileLoadingPath(null);
    setRepoFileError(null);
    setRepoFileByPath({});
    return () => {
      epoch.current++;
    };
  }, [ownerKey, connected]);

  const refresh = useCallback(async () => {
    if (!connected) return;
    const generation = epoch.current;
    const request = ++statusRequest.current;
    setLoading(true);
    try {
      const next = await wsClient.getGitDiffStatus(serverId, {
        targetId: workerId,
        cwd,
      });
      if (generation !== epoch.current || request !== statusRequest.current)
        return;
      setResult({ owner: ownerKey, status: next });
      setError(null);
      setRefreshKey((value) => value + 1);
    } catch (reason: any) {
      if (generation === epoch.current && request === statusRequest.current)
        setError(reason.message || "Could not inspect Git changes.");
    } finally {
      if (generation === epoch.current && request === statusRequest.current)
        setLoading(false);
    }
  }, [connected, serverId, workerId, cwd, ownerKey]);

  useEffect(() => {
    if (!connected) {
      setLoading(false);
      return;
    }
    void refresh();
    // The reader is an explicit snapshot. Background chip refresh does not
    // invalidate review positions while the sheet is open.
    if (visible) return;
    const timer = setInterval(() => void refresh(), 15000);
    return () => clearInterval(timer);
  }, [connected, refresh, visible]);

  const loadPage = useCallback(
    (request: GitDiffPageRequest) =>
      wsClient.getGitDiffPage(serverId, {
        ...request,
        targetId: workerId,
        cwd,
      }),
    [serverId, workerId, cwd],
  );

  const loadRepoPath = useCallback(
    async (path: string = "") => {
      const generation = epoch.current;
      const request = ++browserRequest.current;
      fileRequest.current++;
      setRepoBrowserLoading(true);
      setRepoBrowserError(null);
      setRepoFilePath(null);
      setRepoFileError(null);
      try {
        const payload = await wsClient.getGitRepoEntries(serverId, {
          targetId: workerId,
          cwd,
          path,
        });
        if (generation !== epoch.current || request !== browserRequest.current)
          return;
        setRepoBrowserPath(payload.path);
        setRepoBrowserEntries(payload.entries ?? []);
      } catch (reason: any) {
        if (generation === epoch.current && request === browserRequest.current)
          setRepoBrowserError(
            reason.message || "Could not load repository files.",
          );
      } finally {
        if (generation === epoch.current && request === browserRequest.current)
          setRepoBrowserLoading(false);
      }
    },
    [serverId, workerId, cwd],
  );

  const openRepoFile = useCallback(
    async (path: string) => {
      const generation = epoch.current;
      const request = ++fileRequest.current;
      setRepoFilePath(path);
      setRepoFileError(null);
      setRepoFileLoadingPath(path);
      setRepoFileByPath({});
      try {
        const payload = await wsClient.getGitRepoFileContent(serverId, {
          targetId: workerId,
          cwd,
          path,
        });
        if (generation !== epoch.current || request !== fileRequest.current)
          return;
        setRepoFileByPath({ [path]: payload });
      } catch (reason: any) {
        if (generation === epoch.current && request === fileRequest.current)
          setRepoFileError(reason.message || "Could not load file.");
      } finally {
        if (generation === epoch.current && request === fileRequest.current)
          setRepoFileLoadingPath(null);
      }
    },
    [serverId, workerId, cwd],
  );

  const open = useCallback(() => setVisible(true), []);
  const close = useCallback(() => setVisible(false), []);
  const closeRepoFile = useCallback(() => {
    fileRequest.current++;
    setRepoFilePath(null);
    setRepoFileError(null);
  }, []);
  const goUpRepoPath = useCallback(() => {
    void loadRepoPath(
      repoBrowserPath.includes("/")
        ? repoBrowserPath.slice(0, repoBrowserPath.lastIndexOf("/"))
        : "",
    );
  }, [loadRepoPath, repoBrowserPath]);
  const summary = useMemo<TerminalGitDiffSummary | null>(() => {
    if (!queryEnabled || status?.reason === "not_git_repo") return null;
    const tone: TerminalGitDiffTone = error
      ? "error"
      : loading && !status
        ? "loading"
        : status?.available
          ? status.clean
            ? "clean"
            : "dirty"
          : "loading";
    return {
      label: buildGitDiffChipLabel(status, loading),
      tone,
      additions: status?.additions ?? 0,
      deletions: status?.deletions ?? 0,
      fileCount: status?.file_count ?? 0,
      showStats: Boolean(status?.available),
    };
  }, [queryEnabled, status, loading, error]);

  return {
    queryEnabled,
    actionDisabled: !connected || status?.reason === "not_git_repo",
    chip: summary
      ? { label: summary.label, tone: summary.tone, onPress: open }
      : null,
    summary,
    open,
    sheetProps: {
      visible,
      snapshot: status,
      loading,
      error,
      ownerKey,
      refreshKey,
      loadPage,
      repoBrowserPath,
      repoBrowserEntries,
      repoBrowserLoading,
      repoBrowserError,
      repoFilePath,
      repoFileLoadingPath,
      repoFileError,
      repoFileByPath,
      onClose: close,
      onRefresh: () => {
        void refresh();
      },
      onOpenRepoPath: (path: string) => {
        void loadRepoPath(path);
      },
      onOpenRepoFile: (path: string) => {
        void openRepoFile(path);
      },
      onCloseRepoFile: closeRepoFile,
      onBackRepoPath: goUpRepoPath,
    },
  };
}
