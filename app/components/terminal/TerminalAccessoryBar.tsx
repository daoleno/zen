import React, { useRef } from "react";
import {
  Alert,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as DocumentPicker from "expo-document-picker";
import * as Haptics from "expo-haptics";
import { Typography } from "../../constants/tokens";
import {
  buildTerminalChrome,
  resolveTerminalTheme,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import { buildUploadHeaders, buildUploadUrl } from "../../services/uploads";
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
  gitDiff?: {
    label: string;
    tone: "clean" | "dirty" | "error" | "loading";
    onPress(): void;
  } | null;
  ctrlArmed: boolean;
  onCtrlArmedChange(next: boolean): void;
}

export function TerminalAccessoryBar({
  terminalRef,
  serverUrl,
  daemonId,
  theme,
  gitDiff,
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
          <TouchableOpacity
            accessibilityLabel="Git diff"
            style={[
              styles.gitDiffChip,
              {
                backgroundColor:
                  gitDiff.tone === "dirty"
                    ? chrome.accentSoft
                    : gitDiff.tone === "clean"
                      ? withAlpha(activeTheme.green, 0.14)
                      : chrome.surfaceMuted,
                borderColor:
                  gitDiff.tone === "dirty"
                    ? chrome.borderStrong
                    : chrome.border,
              },
            ]}
            onPress={gitDiff.onPress}
            activeOpacity={0.75}
          >
            <Ionicons
              name={gitDiff.tone === "loading" ? "sync-outline" : "git-branch-outline"}
              size={14}
              color={gitDiff.tone === "dirty" ? chrome.accent : chrome.textMuted}
            />
            <Text
              style={[
                styles.gitDiffChipText,
                {
                  color: gitDiff.tone === "dirty" ? chrome.text : chrome.textMuted,
                },
              ]}
              numberOfLines={1}
            >
              {gitDiff.label}
            </Text>
          </TouchableOpacity>
        ) : null}

        <TouchableOpacity
          accessibilityLabel="Attach"
          style={[
            styles.attachBtn,
            !uploadEnabled && styles.attachBtnDisabled,
          ]}
          onPress={() => void handleFilePick()}
          disabled={!uploadEnabled}
          activeOpacity={0.75}
        >
          <Ionicons
            name="attach-outline"
            size={16}
            color={uploadEnabled ? chrome.textMuted : chrome.textSubtle}
          />
        </TouchableOpacity>

        {SHORTCUT_KEYS.map((key) => {
          if (key.type === "modifier") {
            const active = ctrlArmed;
            return (
              <TouchableOpacity
                key="Ctrl"
                style={styles.shortcutBtn}
                onPress={handleCtrlToggle}
                activeOpacity={0.6}
              >
                <Text style={[styles.shortcutText, { color: active ? chrome.accent : chrome.textMuted }]}>
                  Ctrl
                </Text>
              </TouchableOpacity>
            );
          }

          if (key.type === "hold") {
            return (
              <TouchableOpacity
                key={key.sequence}
                style={styles.shortcutBtn}
                onPressIn={() => handleHoldPressIn(key.sequence)}
                onPressOut={stopRepeat}
                delayLongPress={9999}
                activeOpacity={0.6}
              >
                <Text style={[styles.shortcutText, { color: chrome.textMuted }]}>
                  {key.label}
                </Text>
              </TouchableOpacity>
            );
          }

          // tap key
          return (
            <TouchableOpacity
              key={key.sequence}
              style={styles.shortcutBtn}
              onPress={() => handleTapSequence(key.sequence)}
              activeOpacity={0.6}
            >
              <Text style={[styles.shortcutText, { color: chrome.textMuted }]}>
                {key.label}
              </Text>
            </TouchableOpacity>
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

function withAlpha(hex: string, alpha: number): string {
  const normalized = hex.trim().replace("#", "");
  if (!/^[0-9a-fA-F]{6}$/.test(normalized)) {
    return hex;
  }

  const red = Number.parseInt(normalized.slice(0, 2), 16);
  const green = Number.parseInt(normalized.slice(2, 4), 16);
  const blue = Number.parseInt(normalized.slice(4, 6), 16);
  return `rgba(${red}, ${green}, ${blue}, ${Math.min(Math.max(alpha, 0), 1)})`;
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
  attachBtn: {
    width: 36,
    height: 36,
    marginRight: 2,
    alignItems: "center",
    justifyContent: "center",
  },
  attachBtnDisabled: {
    opacity: 0.35,
  },
  gitDiffChip: {
    maxWidth: 220,
    minHeight: 36,
    marginRight: 4,
    paddingHorizontal: 12,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  gitDiffChipText: {
    flexShrink: 1,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  shortcutBtn: {
    paddingHorizontal: 10,
    height: 36,
    marginRight: 2,
    alignItems: "center",
    justifyContent: "center",
  },
  shortcutText: {
    fontSize: 13,
    fontFamily: Typography.terminalFont,
  },
});
