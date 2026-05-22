import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ConnectionState } from "../../store/agents";
import {
  buildGitDiffChipLabel,
  type GitDiffPatchPayload,
  type GitDiffStatusSnapshot,
  type GitRepoBrowserEntry,
  type GitRepoFileContentPayload,
} from "../../services/gitDiff";
import { wsClient } from "../../services/websocket";

interface UseTerminalGitDiffInput {
  serverId: string;
  agentId: string;
  cwd: string;
  connectionState: ConnectionState;
  hasTerminalRoute: boolean;
  screenFocused: boolean;
}

function hasGitDiffPatchContent(payload?: GitDiffPatchPayload) {
  return Boolean(payload?.sections?.some((section) => section.patch.trim()));
}

export function useTerminalGitDiff({
  serverId,
  agentId,
  cwd,
  connectionState,
  hasTerminalRoute,
  screenFocused,
}: UseTerminalGitDiffInput) {
  const [visible, setVisible] = useState(false);
  const [status, setStatus] = useState<GitDiffStatusSnapshot | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [patchLoadingByPath, setPatchLoadingByPath] = useState<
    Record<string, boolean>
  >({});
  const [patchErrorByPath, setPatchErrorByPath] = useState<
    Record<string, string | undefined>
  >({});
  const [patchByPath, setPatchByPath] = useState<
    Record<string, GitDiffPatchPayload | undefined>
  >({});
  const patchByPathRef = useRef(patchByPath);
  const patchLoadingByPathRef = useRef(patchLoadingByPath);
  const patchErrorByPathRef = useRef(patchErrorByPath);
  const requestRef = useRef(0);
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
  const repoBrowserPathRef = useRef(repoBrowserPath);

  const queryEnabled = Boolean(
    hasTerminalRoute && screenFocused && serverId && agentId && cwd,
  );

  useEffect(() => {
    patchByPathRef.current = patchByPath;
  }, [patchByPath]);

  useEffect(() => {
    patchLoadingByPathRef.current = patchLoadingByPath;
  }, [patchLoadingByPath]);

  useEffect(() => {
    patchErrorByPathRef.current = patchErrorByPath;
  }, [patchErrorByPath]);

  useEffect(() => {
    repoBrowserPathRef.current = repoBrowserPath;
  }, [repoBrowserPath]);

  const resetPatchCache = useCallback(() => {
    patchByPathRef.current = {};
    patchErrorByPathRef.current = {};
    patchLoadingByPathRef.current = {};
    setPatchByPath({});
    setPatchErrorByPath({});
    setPatchLoadingByPath({});
  }, []);

  const resetRepoState = useCallback(() => {
    setRepoBrowserPath("");
    setRepoBrowserEntries([]);
    setRepoBrowserError(null);
    setRepoBrowserLoading(false);
    setRepoFilePath(null);
    setRepoFileError(null);
    setRepoFileLoadingPath(null);
    setRepoFileByPath({});
  }, []);

  const refresh = useCallback(
    async (showLoading: boolean = true) => {
      if (
        !serverId
        || !agentId
        || connectionState !== "connected"
        || !queryEnabled
      ) {
        setStatus(null);
        setError(null);
        resetPatchCache();
        resetRepoState();
        if (showLoading) {
          setLoading(false);
        }
        return;
      }

      const requestId = requestRef.current + 1;
      requestRef.current = requestId;

      if (showLoading) {
        setLoading(true);
        resetPatchCache();
      }

      try {
        const nextStatus = await wsClient.getGitDiffStatus(serverId, {
          targetId: agentId,
          cwd,
        });
        if (requestRef.current !== requestId) return;

        setStatus(nextStatus);
        setError(null);
        setPatchByPath((previous) => {
          if (!nextStatus.available) {
            patchByPathRef.current = {};
            return {};
          }
          const allowed = new Set(
            (nextStatus.files ?? []).map((file) => file.path),
          );
          const next = Object.fromEntries(
            Object.entries(previous).filter(
              ([path, patch]) => allowed.has(path) && hasGitDiffPatchContent(patch),
            ),
          );
          patchByPathRef.current = next;
          return next;
        });
        setPatchErrorByPath((previous) => {
          if (!nextStatus.available) {
            patchErrorByPathRef.current = {};
            return {};
          }
          const allowed = new Set(
            (nextStatus.files ?? []).map((file) => file.path),
          );
          const next = Object.fromEntries(
            Object.entries(previous).filter(([path]) => allowed.has(path)),
          );
          patchErrorByPathRef.current = next;
          return next;
        });
      } catch (nextError: any) {
        if (requestRef.current !== requestId) return;
        setError(nextError?.message || "Could not inspect local git changes.");
      } finally {
        if (showLoading && requestRef.current === requestId) {
          setLoading(false);
        }
      }
    },
    [
      agentId,
      connectionState,
      cwd,
      queryEnabled,
      resetPatchCache,
      resetRepoState,
      serverId,
    ],
  );

  useEffect(() => {
    setVisible(false);
  }, [agentId, serverId]);

  useEffect(() => {
    resetPatchCache();
    resetRepoState();
    setError(null);
  }, [agentId, cwd, resetPatchCache, resetRepoState, serverId]);

  useEffect(() => {
    if (!queryEnabled || connectionState !== "connected") {
      requestRef.current += 1;
      setStatus(null);
      setError(null);
      setLoading(false);
      resetPatchCache();
      resetRepoState();
      return;
    }

    void refresh(true);

    const interval = setInterval(() => {
      void refresh(false);
    }, visible ? 7000 : 15000);

    return () => {
      requestRef.current += 1;
      clearInterval(interval);
    };
  }, [
    connectionState,
    queryEnabled,
    refresh,
    resetPatchCache,
    resetRepoState,
    visible,
  ]);

  const open = useCallback(() => {
    setVisible(true);
    void refresh(true);
  }, [refresh]);

  const close = useCallback(() => {
    setVisible(false);
  }, []);

  const ensurePatch = useCallback(
    async (path: string) => {
      const nextPath = path.trim();
      if (!nextPath || !serverId || !agentId) {
        return;
      }

      if (
        patchByPathRef.current[nextPath]
        || patchLoadingByPathRef.current[nextPath]
        || patchErrorByPathRef.current[nextPath]
      ) {
        return;
      }

      patchLoadingByPathRef.current = {
        ...patchLoadingByPathRef.current,
        [nextPath]: true,
      };
      setPatchLoadingByPath((previous) => ({
        ...previous,
        [nextPath]: true,
      }));
      patchErrorByPathRef.current = {
        ...patchErrorByPathRef.current,
        [nextPath]: undefined,
      };
      setPatchErrorByPath((previous) => ({
        ...previous,
        [nextPath]: undefined,
      }));

      try {
        const payload = await wsClient.getGitDiffPatch(serverId, {
          targetId: agentId,
          cwd,
          path: nextPath,
        });
        patchByPathRef.current = {
          ...patchByPathRef.current,
          [nextPath]: payload,
        };
        setPatchByPath((previous) => ({
          ...previous,
          [nextPath]: payload,
        }));
      } catch (nextError: any) {
        patchErrorByPathRef.current = {
          ...patchErrorByPathRef.current,
          [nextPath]: nextError?.message || "Could not load this patch.",
        };
        setPatchErrorByPath((previous) => ({
          ...previous,
          [nextPath]: nextError?.message || "Could not load this patch.",
        }));
      } finally {
        const nextLoading = { ...patchLoadingByPathRef.current };
        delete nextLoading[nextPath];
        patchLoadingByPathRef.current = nextLoading;
        setPatchLoadingByPath((previous) => {
          const next = { ...previous };
          delete next[nextPath];
          return next;
        });
      }
    },
    [agentId, cwd, serverId],
  );

  const loadRepoPath = useCallback(
    async (path: string = "") => {
      if (!serverId || !agentId || !cwd) {
        return;
      }

      setRepoBrowserLoading(true);
      setRepoBrowserError(null);
      setRepoFilePath(null);
      setRepoFileError(null);

      try {
        const payload = await wsClient.getGitRepoEntries(serverId, {
          targetId: agentId,
          cwd,
          path,
        });
        setRepoBrowserPath(payload.path || "");
        setRepoBrowserEntries(payload.entries ?? []);
      } catch (nextError: any) {
        setRepoBrowserError(nextError?.message || "Could not load repository files.");
      } finally {
        setRepoBrowserLoading(false);
      }
    },
    [agentId, cwd, serverId],
  );

  const openRepoFile = useCallback(
    async (path: string) => {
      const nextPath = path.trim();
      if (!nextPath || !serverId || !agentId || !cwd) {
        return;
      }

      setRepoFilePath(nextPath);
      setRepoFileError(null);

      if (repoFileByPath[nextPath]) {
        return;
      }

      setRepoFileLoadingPath(nextPath);
      try {
        const payload = await wsClient.getGitRepoFileContent(serverId, {
          targetId: agentId,
          cwd,
          path: nextPath,
        });
        setRepoFileByPath((previous) => ({
          ...previous,
          [nextPath]: payload,
        }));
      } catch (nextError: any) {
        setRepoFileError(nextError?.message || "Could not load repository file.");
      } finally {
        setRepoFileLoadingPath((previous) =>
          previous === nextPath ? null : previous,
        );
      }
    },
    [agentId, cwd, repoFileByPath, serverId],
  );

  const closeRepoFile = useCallback(() => {
    setRepoFilePath(null);
    setRepoFileError(null);
  }, []);

  const goUpRepoPath = useCallback(() => {
    const parent = repoBrowserPath.includes("/")
      ? repoBrowserPath.slice(0, repoBrowserPath.lastIndexOf("/"))
      : "";
    void loadRepoPath(parent);
  }, [loadRepoPath, repoBrowserPath]);

  useEffect(() => {
    if (!visible || !status?.available) {
      return;
    }
    void loadRepoPath(repoBrowserPathRef.current || "");
  }, [loadRepoPath, status?.available, status?.repo_root, visible]);

  const chip = useMemo(() => {
    if (!queryEnabled) {
      return null;
    }
    if (status?.reason === "not_git_repo") {
      return null;
    }

    const tone: "clean" | "dirty" | "error" | "loading" =
      loading && !status
        ? "loading"
        : status?.available
          ? status.clean
            ? "clean"
            : "dirty"
          : error
            ? "error"
            : "loading";

    return {
      label: buildGitDiffChipLabel(status, loading),
      tone,
      onPress: open,
    };
  }, [error, loading, open, queryEnabled, status]);

  const sheetProps = useMemo(
    () => ({
      visible,
      snapshot: status,
      loading,
      error,
      patchByPath,
      patchLoadingByPath,
      patchErrorByPath,
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
        void refresh(true);
      },
      onOpenRepoPath: (path: string) => {
        void loadRepoPath(path);
      },
      onOpenRepoFile: (path: string) => {
        void openRepoFile(path);
      },
      onLoadDiffPatch: (path: string) => {
        void ensurePatch(path);
      },
      onCloseRepoFile: closeRepoFile,
      onBackRepoPath: goUpRepoPath,
    }),
    [
      close,
      closeRepoFile,
      ensurePatch,
      error,
      goUpRepoPath,
      loadRepoPath,
      loading,
      openRepoFile,
      patchByPath,
      patchErrorByPath,
      patchLoadingByPath,
      refresh,
      repoBrowserEntries,
      repoBrowserError,
      repoBrowserLoading,
      repoBrowserPath,
      repoFileByPath,
      repoFileError,
      repoFileLoadingPath,
      repoFilePath,
      status,
      visible,
    ],
  );

  return {
    queryEnabled,
    actionDisabled: !queryEnabled || status?.reason === "not_git_repo",
    chip,
    open,
    sheetProps,
  };
}
