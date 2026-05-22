import React, { useRef } from "react";
import {
  Alert,
  Keyboard,
  StyleSheet,
  View,
} from "react-native";
import * as DocumentPicker from "expo-document-picker";
import * as Haptics from "expo-haptics";
import {
  buildTerminalChrome,
  resolveTerminalTheme,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import { buildUploadHeaders, buildUploadUrl } from "../../services/uploads";
import {
  type TerminalAccessoryGitDiff,
} from "./TerminalAccessoryGitDiffChip";
import { TerminalAccessoryControls } from "./TerminalAccessoryControls";
import type { TerminalSurfaceHandle } from "./TerminalSurface";

// Initial delay before repeat begins (matches system key-repeat feel)
const REPEAT_DELAY_MS = 360;
// Interval between repeated inputs once repeat is active
const REPEAT_RATE_MS = 80;

interface TerminalAccessoryBarProps {
  terminalRef: React.RefObject<TerminalSurfaceHandle | null>;
  serverUrl: string;
  daemonId: string;
  theme?: TerminalThemePalette;
  gitDiff?: TerminalAccessoryGitDiff | null;
  keyboardVisible: boolean;
  ctrlArmed: boolean;
  onCtrlArmedChange(next: boolean): void;
}

export function TerminalAccessoryBar({
  terminalRef,
  serverUrl,
  daemonId,
  theme,
  gitDiff,
  keyboardVisible,
  ctrlArmed,
  onCtrlArmedChange,
}: TerminalAccessoryBarProps) {
  const uploadEnabled = !!buildUploadUrl(serverUrl) && !!daemonId.trim();
  const activeTheme = React.useMemo(
    () => theme ?? resolveTerminalTheme(),
    [theme],
  );
  const chrome = React.useMemo(
    () => buildTerminalChrome(activeTheme),
    [activeTheme],
  );

  const repeatDelayRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const repeatIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

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
    try {
      const result = await DocumentPicker.getDocumentAsync({
        type: ["*/*"],
        copyToCacheDirectory: true,
      });
      if (result.canceled || !result.assets?.length) return;

      const asset = result.assets[0];
      const uploadUrl = buildUploadUrl(serverUrl);
      if (!uploadUrl) {
        throw new Error("Server URL is not configured");
      }

      const formData = new FormData();
      formData.append("file", {
        uri: asset.uri,
        name: asset.name || "upload",
        type: asset.mimeType || "application/octet-stream",
      } as any);

      const response = await fetch(uploadUrl, {
        method: "POST",
        headers: await buildUploadHeaders(daemonId),
        body: formData,
      });
      if (!response.ok) {
        throw new Error(`Upload failed (${response.status})`);
      }

      const payload = (await response.json()) as { path?: string };
      if (!payload.path) {
        throw new Error("Upload response missing file path");
      }

      onCtrlArmedChange(false);
      terminalRef.current?.resumeInput();
      sendInput(appendShellPath("", payload.path));
      await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
    } catch (err: any) {
      Alert.alert("Error", err?.message || "Failed to upload file");
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
        keyboardVisible={keyboardVisible}
        ctrlArmed={ctrlArmed}
        chrome={chrome}
        theme={activeTheme}
        gitDiff={gitDiff}
        onUploadPress={() => void handleFilePick()}
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
