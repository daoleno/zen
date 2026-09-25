import { useCallback, useRef } from "react";
import * as Clipboard from "expo-clipboard";
import type {
  GitDiffPage,
  GitDiffPageRequest,
  GitDiffScope,
} from "../../services/gitDiff";
import { useToast } from "../ui/Toast";
import { collectGitDiffPatch } from "./gitDiffActions";

/** Clipboard actions with Toast feedback (Toast also owns the haptic). */
export function useGitDiffClipboard(
  loadPage: (request: GitDiffPageRequest) => Promise<GitDiffPage>,
) {
  const toast = useToast();
  const copyingPatch = useRef(false);
  const copyText = useCallback(
    async (text: string, title: string, detail?: string) => {
      try {
        await Clipboard.setStringAsync(text);
        toast.show({ title, detail, tone: "success" });
      } catch (error) {
        toast.show({
          title: "Couldn't copy",
          detail: error instanceof Error ? error.message : undefined,
          tone: "error",
        });
      }
    },
    [toast],
  );
  const copyPath = useCallback(
    (path: string) => copyText(path, "Path copied", path),
    [copyText],
  );
  const copyPatch = useCallback(
    async (path: string, scope: GitDiffScope) => {
      if (copyingPatch.current) return;
      copyingPatch.current = true;
      try {
        const result = await collectGitDiffPatch(
          loadPage,
          path,
          scope,
          undefined,
          () => toast.show({ title: "Copying diff\u2026", tone: "info" }),
        );
        if (result.ok) {
          await copyText(
            result.text,
            "Diff copied",
            `${result.lines} ${result.lines === 1 ? "line" : "lines"}`,
          );
          return;
        }
        toast.show({
          tone: "error",
          ...(result.reason === "too-large"
            ? {
                title: "Diff too large to copy",
                detail: `${result.total} rows`,
              }
            : result.reason === "changed"
              ? { title: "Diff changed while copying" }
              : { title: "Nothing to copy" }),
        });
      } catch (error) {
        toast.show({
          title: "Couldn't copy diff",
          detail: error instanceof Error ? error.message : undefined,
          tone: "error",
        });
      } finally {
        copyingPatch.current = false;
      }
    },
    [copyText, loadPage, toast],
  );
  return { copyText, copyPath, copyPatch };
}
