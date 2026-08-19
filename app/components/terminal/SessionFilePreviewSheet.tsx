import { Ionicons } from "@expo/vector-icons";
import * as Clipboard from "expo-clipboard";
import React, {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";
import {
  ActivityIndicator,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
  type LayoutChangeEvent,
} from "react-native";
import { Gesture, GestureDetector } from "react-native-gesture-handler";
import Reanimated, {
  useAnimatedStyle,
  useSharedValue,
} from "react-native-reanimated";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import {
  createSessionFileCopyLifecycleOwner,
  SESSION_FILE_COPIED_RESET_MS,
} from "../../services/sessionFilePreviewCopy";
import {
  bindSessionFileRequestToGeneration,
  buildSessionFileBinarySource,
  classifySessionFileRenderer,
  formatSessionFileSize,
  initialSessionFilePreviewState,
  isStaleSessionFileError,
  reduceSessionFilePreviewState,
  sessionFileTooLargeMessage,
  sessionFilePreviewScopeKey,
  type SessionFileBinarySource,
  type SessionFileMetadata,
  type SessionFilePreviewState,
} from "../../services/sessionFilePreview";
import {
  createSessionFileDownloadLifecycleOwner,
  exportSessionFileDownload,
  sessionFileCanDownload,
  sessionFileDownloadFileName,
  sessionFileDownloadMimeType,
  sessionFileDownloadRequest,
  sessionFileDownloadErrorMessage,
  type SessionFileDownloadFeedback,
} from "../../services/sessionFilePreviewDownload";
import { createExpoSessionFileDownloadBackend } from "../../services/sessionFilePreviewDownload.expo";
import { wsClient } from "../../services/websocket";
import { BottomSheetFrame } from "../ui";
import { MessageBody } from "./InterfaceMessageBody";
import { TimelineTextSelectableContext } from "./TimelineTextSelectableContext";
import { SessionFilePreviewContext } from "./SessionFilePreviewContext";
import { SessionFilePdfPreview } from "./SessionFilePdfPreview";

interface SessionFilePreviewSheetProps {
  reference: string | null;
  serverId: string;
  serverUrl: string;
  daemonId: string;
  agentId: string;
  processId?: number;
  startedAt?: number;
  cwd?: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onClose(): void;
}

export function SessionFilePreviewSheet({
  reference,
  serverId,
  serverUrl,
  daemonId,
  agentId,
  processId,
  startedAt,
  cwd,
  chrome,
  theme,
  onClose,
}: SessionFilePreviewSheetProps) {
  const [state, dispatch] = useReducer(
    reduceSessionFilePreviewState,
    initialSessionFilePreviewState,
  );
  const scopeKey = sessionFilePreviewScopeKey({
    serverId,
    serverUrl,
    daemonId,
    agentId,
    processId,
    startedAt,
    cwd,
  });
  const previousScopeRef = useRef(scopeKey);

  useEffect(() => {
    if (previousScopeRef.current === scopeKey) return;
    previousScopeRef.current = scopeKey;
    dispatch({ type: "context_changed" });
    if (reference) onClose();
  }, [onClose, reference, scopeKey]);

  useEffect(() => {
    if (!reference) {
      dispatch({ type: "close" });
      return;
    }
    dispatch({ type: "open", reference });
  }, [reference]);

  useEffect(() => {
    if (!state.reference || state.status !== "loading") return;
    let cancelled = false;
    const load = async () => {
      if (!processId || !startedAt) {
        throw new Error(
          "This Session has no live generation identity. Refresh the Session list and try again.",
        );
      }
      const request = {
        agentId,
        processId,
        startedAt,
        path: state.reference!,
      };
      const metadata = await wsClient.getSessionFileMetadata(serverId, request);
      if (cancelled) return;
      dispatch({ type: "metadata_loaded", metadata });

      if (metadata.tooLarge) {
        dispatch({
          type: "failed",
          message: sessionFileTooLargeMessage(metadata),
          stale: false,
        });
        return;
      }

      const renderer = classifySessionFileRenderer(metadata.kind);
      const generationRequest = bindSessionFileRequestToGeneration(
        request,
        metadata,
      );
      if (renderer === "markdown" || renderer === "text") {
        const text = await wsClient.getSessionFileText(
          serverId,
          generationRequest,
        );
        if (!cancelled) dispatch({ type: "text_loaded", text });
        return;
      }
      if (renderer === "image" || renderer === "pdf") {
        const source = await buildSessionFileBinarySource(
          serverId,
          daemonId,
          generationRequest,
        );
        if (!cancelled) dispatch({ type: "binary_ready", source });
        return;
      }
      if (!cancelled) dispatch({ type: "ready" });
    };
    void load().catch((error) => {
      if (cancelled) return;
      dispatch({
        type: "failed",
        message:
          error instanceof Error
            ? error.message
            : "Could not open the Session file.",
        stale: isStaleSessionFileError(error),
      });
    });
    return () => {
      cancelled = true;
    };
  }, [
    agentId,
    daemonId,
    processId,
    serverId,
    serverUrl,
    startedAt,
    state.reference,
    state.requestEpoch,
    state.status,
  ]);

  const close = useCallback(() => {
    dispatch({ type: "close" });
    onClose();
  }, [onClose]);
  const retry = useCallback(() => dispatch({ type: "retry" }), []);
  const markBinaryFailure = useCallback(() => {
    dispatch({
      type: "failed",
      message:
        "The file stream changed or could not be loaded. Refresh and try again.",
      stale: true,
    });
  }, []);
  const markPdfFailure = useCallback((message: string, stale: boolean) => {
    dispatch({ type: "failed", message, stale });
  }, []);
  const [pathCopied, setPathCopied] = useState(false);
  const [downloadFeedback, setDownloadFeedback] =
    useState<SessionFileDownloadFeedback>("idle");
  const [downloadError, setDownloadError] = useState<string | null>(null);
  const downloadBackend = useMemo(
    () => createExpoSessionFileDownloadBackend(),
    [],
  );
  const downloadOwner = useMemo(
    () =>
      createSessionFileDownloadLifecycleOwner({
        onFeedbackChange: (feedback, error) => {
          setDownloadFeedback(feedback);
          setDownloadError(
            feedback === "failed"
              ? sessionFileDownloadErrorMessage(error)
              : null,
          );
        },
      }),
    [],
  );
  const copyOwner = useMemo(
    () =>
      createSessionFileCopyLifecycleOwner({
        copyText: Clipboard.setStringAsync,
        onCopiedChange: setPathCopied,
        scheduleReset: setTimeout,
        cancelReset: clearTimeout,
        resetDelayMs: SESSION_FILE_COPIED_RESET_MS,
      }),
    [],
  );

  useEffect(() => {
    copyOwner.replaceController();
    downloadOwner.reset();
    setDownloadError(null);
  }, [copyOwner, downloadOwner, state.reference, state.requestEpoch]);

  useEffect(() => {
    return () => {
      copyOwner.dispose();
      downloadOwner.dispose();
    };
  }, [copyOwner, downloadOwner]);

  const copyPath = useCallback(() => {
    const path = state.metadata?.path || state.reference;
    if (path) void copyOwner.copy(path);
  }, [copyOwner, state.metadata?.path, state.reference]);

  const downloadFile = useCallback(() => {
    const metadata = state.metadata;
    if (
      !metadata ||
      !sessionFileCanDownload(metadata) ||
      !processId ||
      !startedAt
    ) {
      return;
    }
    const fileName = sessionFileDownloadFileName(metadata);
    const mimeType = sessionFileDownloadMimeType(metadata.contentType);
    const request = sessionFileDownloadRequest(
      { agentId, processId, startedAt },
      metadata,
    );
    void downloadOwner
      .start(() =>
        exportSessionFileDownload({
          fileName,
          mimeType,
          expectedBytes: metadata.size,
          resolveSource: () =>
            buildSessionFileBinarySource(serverId, daemonId, request),
          backend: downloadBackend,
        }),
      )
      .catch(() => {});
  }, [
    agentId,
    daemonId,
    downloadBackend,
    downloadOwner,
    processId,
    serverId,
    startedAt,
    state.metadata,
  ]);

  return (
    <BottomSheetFrame
      visible={Boolean(reference)}
      maxHeight="92%"
      cardStyle={styles.sheet}
      contentStyle={styles.sheetContent}
      dragToDismiss
      onClose={close}
    >
      <SessionFilePreviewHeader
        state={state}
        chrome={chrome}
        pathCopied={pathCopied}
        downloadFeedback={downloadFeedback}
        downloadError={downloadError}
        onCopyPath={copyPath}
        onDownload={downloadFile}
        onRefresh={retry}
        onClose={close}
      />
      <View style={styles.previewSurface}>
        <SessionFilePreviewBody
          state={state}
          chrome={chrome}
          theme={theme}
          onRetry={retry}
          onBinaryError={markBinaryFailure}
          onPdfError={markPdfFailure}
        />
      </View>
    </BottomSheetFrame>
  );
}

function SessionFilePreviewHeader({
  state,
  chrome,
  pathCopied,
  downloadFeedback,
  downloadError,
  onCopyPath,
  onDownload,
  onRefresh,
  onClose,
}: {
  state: SessionFilePreviewState;
  chrome: TerminalThemeChrome;
  pathCopied: boolean;
  downloadFeedback: SessionFileDownloadFeedback;
  downloadError: string | null;
  onCopyPath(): void;
  onDownload(): void;
  onRefresh(): void;
  onClose(): void;
}) {
  const metadata = state.metadata;
  const title = metadata?.name || pathBaseName(state.reference || "") || "File";
  const meta = metadata
    ? [
        metadata.relativePath,
        sessionFileKindLabel(metadata),
        formatSessionFileSize(metadata.size),
      ]
        .filter(Boolean)
        .join(" · ")
    : state.reference || "Current Session";
  const canDownload = sessionFileCanDownload(metadata);
  const downloadBusy = downloadFeedback === "busy";
  const downloadStatus =
    downloadFeedback === "saved"
      ? "Saved"
      : downloadFeedback === "failed"
        ? `Download failed: ${downloadError || "Unknown error"}`
        : null;
  return (
    <View style={[styles.header, { borderBottomColor: chrome.border }]}>
      <View style={styles.headerCopy}>
        <Text
          accessibilityRole="header"
          numberOfLines={1}
          style={[styles.title, { color: chrome.text }]}
        >
          {title}
        </Text>
        <Text
          numberOfLines={1}
          style={[styles.meta, { color: chrome.textSubtle }]}
        >
          {meta}
        </Text>
      </View>
      {downloadStatus ? (
        <Text
          accessibilityLiveRegion="polite"
          style={[
            styles.downloadStatus,
            {
              color:
                downloadFeedback === "saved" ? chrome.accent : chrome.danger,
            },
          ]}
        >
          {downloadStatus}
        </Text>
      ) : null}
      {state.stale ? (
        <HeaderAction
          label="Refresh changed file"
          icon="refresh-outline"
          chrome={chrome}
          onPress={onRefresh}
        />
      ) : null}
      <HeaderAction
        label={
          downloadBusy
            ? "Downloading file"
            : canDownload
              ? "Download file"
              : "Download unavailable"
        }
        icon="download-outline"
        chrome={chrome}
        onPress={onDownload}
        disabled={!canDownload || downloadBusy}
        busy={downloadBusy}
      />
      <HeaderAction
        label={pathCopied ? "File path copied" : "Copy file path"}
        icon={pathCopied ? "checkmark" : "copy-outline"}
        chrome={chrome}
        onPress={onCopyPath}
        selected={pathCopied}
        accent={pathCopied}
      />
      <HeaderAction
        label="Close file preview"
        icon="close"
        chrome={chrome}
        onPress={onClose}
      />
    </View>
  );
}

function HeaderAction({
  label,
  icon,
  chrome,
  onPress,
  disabled = false,
  busy = false,
  selected = false,
  accent = false,
}: {
  label: string;
  icon: React.ComponentProps<typeof Ionicons>["name"];
  chrome: TerminalThemeChrome;
  onPress(): void;
  disabled?: boolean;
  busy?: boolean;
  selected?: boolean;
  accent?: boolean;
}) {
  const iconColor = accent ? chrome.accent : chrome.textMuted;
  return (
    <TouchableOpacity
      accessibilityLabel={label}
      accessibilityRole="button"
      accessibilityState={{ disabled, selected, busy }}
      activeOpacity={0.75}
      disabled={disabled}
      onPress={onPress}
      style={[
        styles.headerAction,
        { backgroundColor: chrome.surfaceMuted, opacity: disabled ? 0.45 : 1 },
      ]}
    >
      {busy ? (
        <ActivityIndicator size="small" color={chrome.textMuted} />
      ) : (
        <Ionicons name={icon} size={18} color={iconColor} />
      )}
    </TouchableOpacity>
  );
}

function SessionFilePreviewBody({
  state,
  chrome,
  theme,
  onRetry,
  onBinaryError,
  onPdfError,
}: {
  state: SessionFilePreviewState;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onRetry(): void;
  onBinaryError(): void;
  onPdfError(message: string, stale: boolean): void;
}) {
  if (state.status === "loading") {
    return (
      <PreviewState
        chrome={chrome}
        icon={<ActivityIndicator color={chrome.accent} />}
        title="Opening file"
        detail="Resolving from the live Session."
      />
    );
  }
  if (state.status === "error") {
    return (
      <PreviewState
        chrome={chrome}
        icon={
          <Ionicons name="warning-outline" size={24} color={chrome.danger} />
        }
        title={
          state.stale
            ? "File changed"
            : state.metadata?.tooLarge
              ? "File too large"
              : "Could not open file"
        }
        detail={state.error || "The file is unavailable."}
        action="Refresh"
        onAction={onRetry}
      />
    );
  }
  if (!state.metadata) return null;

  switch (classifySessionFileRenderer(state.metadata.kind)) {
    case "markdown":
      return state.text ? (
        <ScrollView
          style={styles.scroll}
          contentContainerStyle={styles.markdownContent}
          showsVerticalScrollIndicator
        >
          <TimelineTextSelectableContext.Provider value={{ selectable: true }}>
            <SessionFilePreviewContext.Provider value={null}>
              <MessageBody
                value={state.text.content}
                chrome={chrome}
                theme={theme}
              />
            </SessionFilePreviewContext.Provider>
          </TimelineTextSelectableContext.Provider>
          <TruncationNote preview={state.text} chrome={chrome} />
        </ScrollView>
      ) : null;
    case "text":
      return state.text ? (
        <ScrollView style={styles.scroll} showsVerticalScrollIndicator>
          <ScrollView
            horizontal
            nestedScrollEnabled
            contentContainerStyle={styles.textScrollContent}
            showsHorizontalScrollIndicator
          >
            <Text
              selectable
              style={[styles.textPreview, { color: chrome.text }]}
            >
              {state.text.content}
            </Text>
          </ScrollView>
          <TruncationNote preview={state.text} chrome={chrome} />
        </ScrollView>
      ) : null;
    case "image":
      return state.binarySource ? (
        <SessionFileImagePreview
          source={state.binarySource}
          chrome={chrome}
          onError={onBinaryError}
        />
      ) : null;
    case "pdf":
      return state.binarySource ? (
        <SessionFilePdfPreview
          source={state.binarySource}
          generation={state.metadata.generation}
          expectedBytes={state.metadata.size}
          chrome={chrome}
          onError={onPdfError}
        />
      ) : null;
    default:
      return (
        <PreviewState
          chrome={chrome}
          icon={
            <Ionicons
              name="document-outline"
              size={25}
              color={chrome.textMuted}
            />
          }
          title="Preview not supported"
          detail="This binary type has metadata only. Zen does not read it into memory or send it through JSON."
        />
      );
  }
}

function SessionFileImagePreview({
  source,
  chrome,
  onError,
}: {
  source: SessionFileBinarySource;
  chrome: TerminalThemeChrome;
  onError(): void;
}) {
  const scale = useSharedValue(1);
  const savedScale = useSharedValue(1);
  const translateX = useSharedValue(0);
  const translateY = useSharedValue(0);
  const savedX = useSharedValue(0);
  const savedY = useSharedValue(0);
  const width = useSharedValue(1);
  const height = useSharedValue(1);

  useEffect(() => {
    scale.value = 1;
    savedScale.value = 1;
    translateX.value = 0;
    translateY.value = 0;
    savedX.value = 0;
    savedY.value = 0;
  }, [savedScale, savedX, savedY, scale, source.uri, translateX, translateY]);

  const pinch = useMemo(
    () =>
      Gesture.Pinch()
        .onUpdate((event) => {
          scale.value = Math.max(
            1,
            Math.min(4, savedScale.value * event.scale),
          );
        })
        .onEnd(() => {
          savedScale.value = scale.value;
          const maxX = (width.value * (scale.value - 1)) / 2;
          const maxY = (height.value * (scale.value - 1)) / 2;
          translateX.value = Math.max(-maxX, Math.min(maxX, translateX.value));
          translateY.value = Math.max(-maxY, Math.min(maxY, translateY.value));
          savedX.value = translateX.value;
          savedY.value = translateY.value;
        }),
    [height, savedScale, savedX, savedY, scale, translateX, translateY, width],
  );
  const pan = useMemo(
    () =>
      Gesture.Pan()
        .minDistance(4)
        .onUpdate((event) => {
          const maxX = (width.value * (scale.value - 1)) / 2;
          const maxY = (height.value * (scale.value - 1)) / 2;
          translateX.value = Math.max(
            -maxX,
            Math.min(maxX, savedX.value + event.translationX),
          );
          translateY.value = Math.max(
            -maxY,
            Math.min(maxY, savedY.value + event.translationY),
          );
        })
        .onEnd(() => {
          savedX.value = translateX.value;
          savedY.value = translateY.value;
        }),
    [height, savedX, savedY, scale, translateX, translateY, width],
  );
  const gesture = useMemo(() => Gesture.Simultaneous(pinch, pan), [pan, pinch]);
  const imageStyle = useAnimatedStyle(() => ({
    transform: [
      { translateX: translateX.value },
      { translateY: translateY.value },
      { scale: scale.value },
    ],
  }));
  const handleLayout = useCallback(
    (event: LayoutChangeEvent) => {
      width.value = Math.max(1, event.nativeEvent.layout.width);
      height.value = Math.max(1, event.nativeEvent.layout.height);
    },
    [height, width],
  );

  return (
    <GestureDetector gesture={gesture}>
      <View
        accessibilityLabel="Zoomable image preview"
        onLayout={handleLayout}
        style={[styles.imageStage, { backgroundColor: chrome.surfaceMuted }]}
      >
        <Reanimated.Image
          source={{ uri: source.uri, headers: source.headers }}
          resizeMode="contain"
          onError={onError}
          style={[styles.image, imageStyle]}
        />
      </View>
    </GestureDetector>
  );
}

function PreviewState({
  chrome,
  icon,
  title,
  detail,
  action,
  onAction,
}: {
  chrome: TerminalThemeChrome;
  icon: React.ReactNode;
  title: string;
  detail: string;
  action?: string;
  onAction?: () => void;
}) {
  return (
    <View style={styles.state}>
      {icon}
      <Text style={[styles.stateTitle, { color: chrome.text }]}>{title}</Text>
      <Text
        selectable
        style={[styles.stateDetail, { color: chrome.textMuted }]}
      >
        {detail}
      </Text>
      {action && onAction ? (
        <TouchableOpacity
          accessibilityRole="button"
          activeOpacity={0.78}
          onPress={onAction}
          style={[styles.retry, { backgroundColor: chrome.accentSoft }]}
        >
          <Text style={[styles.retryText, { color: chrome.accent }]}>
            {action}
          </Text>
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

function TruncationNote({
  preview,
  chrome,
}: {
  preview: { truncated: boolean; bytesRead: number };
  chrome: TerminalThemeChrome;
}) {
  if (!preview.truncated) return null;
  return (
    <Text selectable style={[styles.truncation, { color: chrome.textSubtle }]}>
      Preview limited to the first {formatSessionFileSize(preview.bytesRead)}.
    </Text>
  );
}

function sessionFileKindLabel(metadata: SessionFileMetadata): string {
  switch (metadata.kind) {
    case "markdown":
      return "Markdown";
    case "text":
      return metadata.contentType.includes("json")
        ? "JSON"
        : metadata.contentType.includes("yaml")
          ? "YAML"
          : metadata.name.toLowerCase().endsWith(".log")
            ? "Log"
            : "Text";
    case "image":
      return "Image";
    case "pdf":
      return "PDF";
    default:
      return "Binary";
  }
}

function pathBaseName(path: string): string {
  return path.replace(/\\/g, "/").split("/").at(-1) || "";
}

const styles = StyleSheet.create({
  sheet: {
    height: "92%",
    paddingHorizontal: 0,
    paddingBottom: 0,
    overflow: "hidden",
  },
  sheetContent: {
    flex: 1,
    minHeight: 0,
  },
  header: {
    minHeight: 54,
    paddingHorizontal: 14,
    paddingBottom: 10,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  headerCopy: {
    flex: 1,
    minWidth: 0,
  },
  downloadStatus: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 11,
    flexShrink: 1,
  },
  title: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 15,
    lineHeight: 19,
  },
  meta: {
    fontFamily: Typography.terminalFont,
    fontSize: 10,
    lineHeight: 14,
    marginTop: 1,
  },
  headerAction: {
    width: 34,
    height: 34,
    borderRadius: 17,
    alignItems: "center",
    justifyContent: "center",
  },
  previewSurface: {
    flex: 1,
    minHeight: 0,
  },
  scroll: {
    flex: 1,
    minHeight: 0,
  },
  markdownContent: {
    paddingHorizontal: 16,
    paddingVertical: 16,
  },
  textScrollContent: {
    minWidth: "100%",
    paddingHorizontal: 16,
    paddingTop: 15,
    paddingBottom: 18,
  },
  textPreview: {
    fontFamily: Typography.terminalFont,
    fontSize: 12,
    lineHeight: 19,
  },
  truncation: {
    paddingHorizontal: 16,
    paddingVertical: 12,
    fontFamily: Typography.uiFont,
    fontSize: 11,
    lineHeight: 16,
  },
  imageStage: {
    flex: 1,
    minHeight: 0,
    overflow: "hidden",
  },
  image: {
    width: "100%",
    height: "100%",
  },
  state: {
    flex: 1,
    paddingHorizontal: 28,
    paddingVertical: 36,
    alignItems: "center",
    justifyContent: "center",
  },
  stateTitle: {
    marginTop: 12,
    fontFamily: Typography.uiFontMedium,
    fontSize: 16,
    lineHeight: 21,
    textAlign: "center",
  },
  stateDetail: {
    maxWidth: 390,
    marginTop: 6,
    fontFamily: Typography.uiFont,
    fontSize: 13,
    lineHeight: 19,
    textAlign: "center",
  },
  retry: {
    marginTop: 16,
    minHeight: 36,
    paddingHorizontal: 15,
    borderRadius: 18,
    alignItems: "center",
    justifyContent: "center",
  },
  retryText: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 13,
  },
});
