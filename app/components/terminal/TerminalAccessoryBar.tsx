import React from "react";
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

type ShortcutKey =
  | { label: "Ctrl"; type: "modifier" }
  | { label: string; type: "sequence"; sequence: string };

const SHORTCUT_KEYS: readonly ShortcutKey[] = [
  { label: "Ctrl", type: "modifier" },
  { label: "Esc", type: "sequence", sequence: "\x1b" },
  { label: "Tab", type: "sequence", sequence: "\t" },
  { label: "⌃B", type: "sequence", sequence: "\x02" },
  { label: "⌃C", type: "sequence", sequence: "\x03" },
  { label: "⌃D", type: "sequence", sequence: "\x04" },
  { label: "←", type: "sequence", sequence: "\x1b[D" },
  { label: "↑", type: "sequence", sequence: "\x1b[A" },
  { label: "↓", type: "sequence", sequence: "\x1b[B" },
  { label: "→", type: "sequence", sequence: "\x1b[C" },
];

interface TerminalAccessoryBarProps {
  terminalRef: React.RefObject<TerminalSurfaceHandle | null>;
  serverUrl: string;
  daemonId: string;
  theme?: TerminalThemePalette;
  ctrlArmed: boolean;
  onCtrlArmedChange(next: boolean): void;
}

export function TerminalAccessoryBar({
  terminalRef,
  serverUrl,
  daemonId,
  theme,
  ctrlArmed,
  onCtrlArmedChange,
}: TerminalAccessoryBarProps) {
  const uploadEnabled = !!buildUploadUrl(serverUrl) && !!daemonId.trim();
  const chrome = React.useMemo(
    () => buildTerminalChrome(theme ?? resolveTerminalTheme()),
    [theme],
  );

  const sendInput = (data: string) => {
    terminalRef.current?.sendInput(data);
  };

  const handleCtrlToggle = () => {
    onCtrlArmedChange(!ctrlArmed);
  };

  const handleShortcut = (sequence: string) => {
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
        <TouchableOpacity
          accessibilityLabel="Attach"
          style={[
            styles.attachBtn,
            {
              backgroundColor: chrome.surface,
              borderColor: chrome.border,
            },
            !uploadEnabled && styles.attachBtnDisabled,
          ]}
          onPress={() => void handleFilePick()}
          disabled={!uploadEnabled}
          activeOpacity={0.82}
        >
          <Ionicons
            name="attach-outline"
            size={18}
            color={uploadEnabled ? chrome.text : chrome.textSubtle}
          />
        </TouchableOpacity>

        {SHORTCUT_KEYS.map((key) => {
          const isModifier = key.type === "modifier";
          const active = isModifier && ctrlArmed;
          return (
            <TouchableOpacity
              key={key.type === "sequence" ? key.sequence : key.label}
              style={[
                styles.shortcutBtn,
                {
                  backgroundColor: chrome.surface,
                  borderColor: chrome.border,
                },
                active && {
                  backgroundColor: chrome.accentSoft,
                  borderColor: chrome.borderStrong,
                },
              ]}
              onPress={() => {
                if (key.type === "modifier") {
                  handleCtrlToggle();
                  return;
                }
                handleShortcut(key.sequence);
              }}
              activeOpacity={0.82}
            >
              <Text
                style={[
                  styles.shortcutText,
                  { color: active ? chrome.accent : chrome.text },
                ]}
              >
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

const styles = StyleSheet.create({
  container: {
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingBottom: 4,
  },
  shortcutRow: {
    paddingTop: 8,
    paddingBottom: 8,
  },
  shortcutRowContent: {
    paddingLeft: 0,
    paddingRight: 18,
  },
  attachBtn: {
    width: 36,
    height: 36,
    borderRadius: 10,
    marginRight: 6,
    alignItems: "center",
    justifyContent: "center",
    borderWidth: 1,
  },
  attachBtnDisabled: {
    opacity: 0.45,
  },
  shortcutBtn: {
    paddingHorizontal: 12,
    height: 36,
    borderRadius: 10,
    marginRight: 6,
    alignItems: "center",
    justifyContent: "center",
    borderWidth: 1,
  },
  shortcutText: {
    fontSize: 12,
    fontFamily: Typography.terminalFont,
  },
});
