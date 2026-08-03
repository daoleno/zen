import React, { useRef } from "react";
import { Alert, Keyboard, StyleSheet, View } from "react-native";
import * as Haptics from "expo-haptics";
import {
  buildTerminalChrome,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import { CurrentAttachmentUpload } from "../../services/currentAttachmentUpload";
import {
  buildUploadUrl,
  createAttachmentUploadOperation,
  pickUploadDocument,
  resolveServerUploadTarget,
  type ActiveAttachmentUpload,
} from "../../services/uploads";
import { TerminalAccessoryControls } from "./TerminalAccessoryControls";
import type { TerminalSurfaceHandle } from "./TerminalSurface";

// Initial delay before repeat begins (matches system key-repeat feel)
const REPEAT_DELAY_MS = 360;
// Interval between repeated inputs once repeat is active
const REPEAT_RATE_MS = 80;

interface TerminalAccessoryBarProps {
  terminalRef: React.RefObject<TerminalSurfaceHandle | null>;
  uploadOwnerKey: string | null;
  serverId: string;
  serverUrl: string;
  daemonId: string;
  theme: TerminalThemePalette;
  keyboardVisible: boolean;
  ctrlArmed: boolean;
  onCtrlArmedChange(next: boolean): void;
}

export function TerminalAccessoryBar({
  terminalRef,
  uploadOwnerKey,
  serverId,
  serverUrl,
  daemonId,
  theme,
  keyboardVisible,
  ctrlArmed,
  onCtrlArmedChange,
}: TerminalAccessoryBarProps) {
  const uploadConfigured = !!buildUploadUrl(serverUrl) && !!daemonId.trim();
  const chrome = React.useMemo(() => buildTerminalChrome(theme), [theme]);

  const repeatDelayRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const repeatIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const uploadOwnerRef = useRef(new CurrentAttachmentUpload());
  const selectionGenerationRef = useRef(0);
  const [selecting, setSelecting] = React.useState(false);
  const [activeUpload, setActiveUpload] =
    React.useState<ActiveAttachmentUpload | null>(null);
  const uploadEnabled = uploadConfigured && !selecting && !activeUpload;

  React.useEffect(() => {
    setSelecting(false);
    setActiveUpload(null);
    return () => {
      selectionGenerationRef.current += 1;
      uploadOwnerRef.current.cancel();
    };
  }, [daemonId, serverUrl, uploadOwnerKey]);

  const sendInput = (data: string) => {
    terminalRef.current?.sendInput(data);
  };

  const stopRepeat = () => {
    if (repeatDelayRef.current !== null) {
      clearTimeout(repeatDelayRef.current);
      repeatDelayRef.current = null;
    }
    if (repeatIntervalRef.current !== null) {
      clearInterval(repeatIntervalRef.current);
      repeatIntervalRef.current = null;
    }
  };

  // For hold keys: send immediately on press-in, then start repeat after delay.
  const handleHoldPressIn = (sequence: string) => {
    sendInput(sequence);
    repeatDelayRef.current = setTimeout(() => {
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
      repeatIntervalRef.current = setInterval(() => {
        sendInput(sequence);
      }, REPEAT_RATE_MS);
    }, REPEAT_DELAY_MS);
  };

  const handleCtrlToggle = () => {
    onCtrlArmedChange(!ctrlArmed);
  };

  const handleKeyboardToggle = () => {
    onCtrlArmedChange(false);
    if (keyboardVisible) {
      terminalRef.current?.blur();
      Keyboard.dismiss();
      return;
    }

    terminalRef.current?.resumeInput();
  };

  // For tap keys: send on press (after release), consistent with modifier toggle.
  const handleTapSequence = (sequence: string) => {
    onCtrlArmedChange(false);
    sendInput(sequence);
  };

  const handleFilePick = async () => {
    if (!uploadEnabled) {
      return;
    }
    const selectionGeneration = selectionGenerationRef.current + 1;
    selectionGenerationRef.current = selectionGeneration;
    setSelecting(true);
    let handle: ReturnType<CurrentAttachmentUpload["start"]> | null = null;
    try {
      const asset = await pickUploadDocument();
      if (selectionGenerationRef.current !== selectionGeneration || !asset) {
        return;
      }

      const server = await resolveServerUploadTarget(serverId);
      if (selectionGenerationRef.current !== selectionGeneration) {
        return;
      }
      setSelecting(false);
      setActiveUpload({ name: asset.name || "upload", progress: null });
      handle = uploadOwnerRef.current.start(
        (onProgress) =>
          createAttachmentUploadOperation(asset, server, {
            onProgress,
          }),
        (progress) => {
          setActiveUpload((current) =>
            current ? { ...current, progress } : current,
          );
        },
      );
      const attachment = await handle.result;
      if (!uploadOwnerRef.current.finish(handle)) {
        return;
      }
      setActiveUpload(null);

      onCtrlArmedChange(false);
      terminalRef.current?.resumeInput();
      sendInput(appendShellPath("", attachment.path));
      await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
    } catch (err: any) {
      if (handle && !uploadOwnerRef.current.finish(handle)) {
        return;
      }
      setActiveUpload(null);
      Alert.alert("Error", err?.message || "Failed to upload file");
    } finally {
      if (selectionGenerationRef.current === selectionGeneration) {
        setSelecting(false);
      }
    }
  };

  const handleCancelUpload = () => {
    selectionGenerationRef.current += 1;
    const cancellationError = uploadOwnerRef.current.cancel();
    setSelecting(false);
    setActiveUpload(null);
    if (cancellationError) {
      Alert.alert(
        "Cancel failed",
        cancellationError.message || "Could not cancel this upload",
      );
    }
  };

  return (
    <View
      style={[
        styles.container,
        {
          backgroundColor: chrome.appBackground,
          borderTopColor: chrome.border,
        },
      ]}
    >
      <TerminalAccessoryControls
        uploadEnabled={uploadEnabled}
        activeUpload={activeUpload}
        keyboardVisible={keyboardVisible}
        ctrlArmed={ctrlArmed}
        chrome={chrome}
        onUploadPress={() => void handleFilePick()}
        onCancelUpload={handleCancelUpload}
        onKeyboardToggle={handleKeyboardToggle}
        onCtrlToggle={handleCtrlToggle}
        onHoldPressIn={handleHoldPressIn}
        onHoldPressOut={stopRepeat}
        onTapSequence={handleTapSequence}
      />
    </View>
  );
}

function appendShellPath(current: string, path: string): string {
  const quoted = shellQuote(path);
  return current.trim() ? `${current} ${quoted}` : quoted;
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `"'"'`)}'`;
}

const styles = StyleSheet.create({
  container: {
    borderTopWidth: StyleSheet.hairlineWidth,
  },
});
