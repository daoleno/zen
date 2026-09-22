import React, { useRef } from "react";
import { Alert, Keyboard, StyleSheet, View } from "react-native";
import * as Haptics from "expo-haptics";
import {
  buildTerminalChrome,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import { buildUploadUrl } from "../../services/uploads";
import { useInterfaceComposerAttachments } from "./useInterfaceComposerAttachments";
import { InterfaceComposerAttachmentRail } from "./InterfaceComposerAttachmentRail";
import type { ComposerAttachment } from "./InterfaceChatSession";
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
  const [attachments, setAttachments] = React.useState<ComposerAttachment[]>([]);
  const insertedRef = useRef(new Set<string>());
  const focusComposer = React.useCallback(() => terminalRef.current?.resumeInput(), [terminalRef]);
  const { activeUpload, canAttach: uploadEnabled, handleUploadAttachment: handleFilePick, cancelUpload: handleCancelUpload, removeAttachment } = useInterfaceComposerAttachments({
    serverId, ownerKey: uploadOwnerKey || "", attachments,
    connectionState: uploadConfigured ? "connected" : "offline", setAttachments, focusComposer,
  });
  React.useEffect(() => { setAttachments([]); insertedRef.current.clear(); }, [serverId, uploadOwnerKey]);
  React.useEffect(() => {
    for (const attachment of attachments) {
      if (attachment.uploadStatus !== "ready" || insertedRef.current.has(attachment.id)) continue;
      insertedRef.current.add(attachment.id);
      terminalRef.current?.sendInput(appendShellPath("", attachment.path));
    }
  }, [attachments, terminalRef]);

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
      <InterfaceComposerAttachmentRail attachments={attachments} activeUpload={null} chrome={chrome} onRemoveAttachment={removeAttachment} onCancelUpload={handleCancelUpload} />
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
