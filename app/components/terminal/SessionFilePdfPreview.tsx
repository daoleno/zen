import { File, Paths } from "expo-file-system";
import React, { useEffect, useRef, useState } from "react";
import { ActivityIndicator, StyleSheet, Text, View } from "react-native";
import PdfRendererView from "react-native-pdf-renderer";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { SessionFileBinarySource } from "../../services/sessionFilePreview";
import {
  createSessionPdfStage,
  SessionPdfStageError,
  type SessionPdfStagingBackend,
  type SessionPdfStagingFile,
} from "../../services/sessionPdfStaging";

const expoPdfStagingBackend: SessionPdfStagingBackend = {
  createTarget(name) {
    return new File(Paths.cache, name);
  },
  download(uri, target, options) {
    return File.downloadFileAsync(
      uri,
      target as File,
      options,
    ) as Promise<SessionPdfStagingFile>;
  },
};

interface SessionFilePdfPreviewProps {
  source: SessionFileBinarySource;
  generation: string;
  expectedBytes: number;
  chrome: TerminalThemeChrome;
  onError(message: string, stale: boolean): void;
}

export function SessionFilePdfPreview({
  source,
  generation,
  expectedBytes,
  chrome,
  onError,
}: SessionFilePdfPreviewProps) {
  const ownerRef = useRef(
    `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`,
  );
  const downloadEpochRef = useRef(0);
  const [localUri, setLocalUri] = useState<string | null>(null);
  const [page, setPage] = useState<{ current: number; total: number } | null>(
    null,
  );

  useEffect(() => {
    const downloadEpoch = downloadEpochRef.current + 1;
    downloadEpochRef.current = downloadEpoch;
    setLocalUri(null);
    setPage(null);

    const operation = createSessionPdfStage(
      {
        uri: source.uri,
        headers: source.headers,
        generation,
        expectedBytes,
        owner: ownerRef.current,
        epoch: downloadEpoch,
      },
      expoPdfStagingBackend,
    );
    void operation.result.then(setLocalUri).catch((error) => {
      if (error instanceof SessionPdfStageError && error.cancelled) return;
      onError(
        error instanceof Error
          ? error.message
          : "Could not prepare the PDF preview.",
        error instanceof SessionPdfStageError && error.stale,
      );
    });

    return operation.dispose;
  }, [expectedBytes, generation, onError, source.headers, source.uri]);

  if (!localUri) {
    return (
      <View style={styles.loading}>
        <ActivityIndicator color={chrome.accent} />
        <Text style={[styles.loadingLabel, { color: chrome.textMuted }]}>
          Preparing PDF
        </Text>
      </View>
    );
  }

  return (
    <View
      accessibilityLabel="PDF preview"
      style={[styles.surface, { backgroundColor: chrome.surfaceMuted }]}
    >
      <PdfRendererView
        source={localUri}
        distanceBetweenPages={10}
        maxZoom={4}
        maxPageResolution={2048}
        onPageChange={(current, total) => {
          setPage({ current: Math.min(total, current + 1), total });
        }}
        onError={() =>
          onError("This PDF could not be rendered on this device.", false)
        }
        style={styles.pdf}
      />
      {page?.total ? (
        <View style={[styles.pageBadge, { backgroundColor: chrome.surface }]}>
          <Text style={[styles.pageLabel, { color: chrome.textMuted }]}>
            {page.current}/{page.total}
          </Text>
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  surface: {
    flex: 1,
    minHeight: 0,
    overflow: "hidden",
  },
  pdf: {
    flex: 1,
    minHeight: 0,
    backgroundColor: "transparent",
  },
  loading: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    gap: 9,
  },
  loadingLabel: {
    fontFamily: Typography.uiFont,
    fontSize: 12,
  },
  pageBadge: {
    position: "absolute",
    right: 12,
    bottom: 12,
    minWidth: 46,
    height: 28,
    paddingHorizontal: 9,
    borderRadius: 14,
    alignItems: "center",
    justifyContent: "center",
  },
  pageLabel: {
    fontFamily: Typography.terminalFont,
    fontSize: 11,
  },
});
