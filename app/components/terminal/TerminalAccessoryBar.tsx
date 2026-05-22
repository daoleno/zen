import React, { useRef } from "react";
import {
  Alert,
  Keyboard,
  ScrollView,
  StyleSheet,
  View,
} from "react-native";
import { Ionicons, MaterialCommunityIcons } from "@expo/vector-icons";
import * as DocumentPicker from "expo-document-picker";
import * as Haptics from "expo-haptics";
import {
  buildTerminalChrome,
  resolveTerminalTheme,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import { buildUploadHeaders, buildUploadUrl } from "../../services/uploads";
import {
  TerminalAccessoryGitDiffChip,
  type TerminalAccessoryGitDiff,
} from "./TerminalAccessoryGitDiffChip";
import { TerminalAccessoryIconButton } from "./TerminalAccessoryIconButton";
import { TerminalAccessoryShortcutButton } from "./TerminalAccessoryShortcutButton";
import type { TerminalSurfaceHandle } from "./TerminalSurface";

// Keys that fire once per tap
type TapKey =
  | { label: "Ctrl"; type: "modifier" }
  | { label: string; type: "tap"; sequence: string };

// Keys that repeat while held
type HoldKey = { label: string; type: "hold"; sequence: string };

type ShortcutKey = TapKey | HoldKey;

const SHORTCUT_KEYS: readonly ShortcutKey[] = [
  { label: "Ctrl", type: "modifier" },
  { label: "Esc", type: "tap", sequence: "\x1b" },
  { label: "Tab", type: "tap", sequence: "\t" },
  { label: "⌃B", type: "tap", sequence: "\x02" },
  { label: "⌃C", type: "tap", sequence: "\x03" },
  { label: "⌃D", type: "tap", sequence: "\x04" },
  // Arrow keys repeat on hold
  { label: "←", type: "hold", sequence: "\x1b[D" },
  { label: "↑", type: "hold", sequence: "\x1b[A" },
  { label: "↓", type: "hold", sequence: "\x1b[B" },
  { label: "→", type: "hold", sequence: "\x1b[C" },
];

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
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        keyboardShouldPersistTaps="handled"
        style={styles.shortcutRow}
        contentContainerStyle={styles.shortcutRowContent}
      >
        {gitDiff ? (
          <TerminalAccessoryGitDiffChip
            gitDiff={gitDiff}
            chrome={chrome}
            theme={activeTheme}
          />
        ) : null}

        <TerminalAccessoryIconButton
          accessibilityLabel="Attach"
          onPress={() => void handleFilePick()}
          disabled={!uploadEnabled}
        >
          <Ionicons
            name="attach-outline"
            size={16}
            color={uploadEnabled ? chrome.textMuted : chrome.textSubtle}
          />
        </TerminalAccessoryIconButton>

        <TerminalAccessoryIconButton
          accessibilityLabel={keyboardVisible ? "Hide keyboard" : "Show keyboard"}
          accessibilityState={{ selected: keyboardVisible }}
          onPress={handleKeyboardToggle}
        >
          <MaterialCommunityIcons
            name="keyboard-outline"
            size={18}
            color={keyboardVisible ? chrome.accent : chrome.textMuted}
          />
        </TerminalAccessoryIconButton>

        {SHORTCUT_KEYS.map((key) => {
          if (key.type === "modifier") {
            const active = ctrlArmed;
            return (
              <TerminalAccessoryShortcutButton
                key="Ctrl"
                label="Ctrl"
                chrome={chrome}
                active={active}
                onPress={handleCtrlToggle}
              />
            );
          }

          if (key.type === "hold") {
            return (
              <TerminalAccessoryShortcutButton
                key={key.sequence}
                label={key.label}
                chrome={chrome}
                onPressIn={() => handleHoldPressIn(key.sequence)}
                onPressOut={stopRepeat}
                delayLongPress={9999}
              />
            );
          }

          // tap key
          return (
            <TerminalAccessoryShortcutButton
              key={key.sequence}
              label={key.label}
              chrome={chrome}
              onPress={() => handleTapSequence(key.sequence)}
            />
          );
        })}
      </ScrollView>
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
  shortcutRow: {
    paddingTop: 3,
    paddingBottom: 3,
  },
  shortcutRowContent: {
    paddingLeft: 12,
    paddingRight: 12,
  },
});
