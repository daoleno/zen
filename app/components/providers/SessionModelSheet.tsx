import React, { useMemo } from "react";
import {
  ActivityIndicator,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale } from "../../constants/tokens";
import type {
  ProviderError,
  ProviderModelChoice,
  ProviderSessionSelection,
} from "../../services/providers";
import type { MenuAnchorLayout } from "../terminal/screen/TerminalScreenModel";

export type SessionModelChoice = {
  connectionId: string;
  modelId: string;
};

export const MODEL_MENU_WIDTH = 288;
const MODEL_MENU_MAX_HEIGHT = 380;
const MODEL_MENU_MARGIN = 12;
const MODEL_MENU_ANCHOR_GAP = 8;

export function buildModelMenuPosition(input: {
  anchor: MenuAnchorLayout | null;
  windowWidth: number;
  windowHeight: number;
  menuWidth: number;
  menuHeight: number;
}): { left: number; top: number; maxHeight: number } {
  const {
    anchor,
    windowWidth,
    windowHeight,
    menuWidth,
    menuHeight,
  } = input;
  const anchorX = anchor?.x ?? windowWidth - MODEL_MENU_MARGIN - menuWidth;
  const anchorY = anchor?.y ?? 0;
  const anchorHeight = anchor?.height ?? 0;

  // Upward-opening: sit above the anchor when it fits, else below it.
  const preferredAbove = anchorY - menuHeight - MODEL_MENU_ANCHOR_GAP;
  const below = anchorY + anchorHeight + MODEL_MENU_ANCHOR_GAP;
  let top = preferredAbove;
  if (top < MODEL_MENU_MARGIN) {
    top = Math.min(below, windowHeight - menuHeight - MODEL_MENU_MARGIN);
  }
  top = clamp(top, MODEL_MENU_MARGIN, Math.max(MODEL_MENU_MARGIN, windowHeight - MODEL_MENU_MARGIN - menuHeight));

  const preferredLeft = anchorX + (anchor?.width ?? 0) - menuWidth;
  const maxLeft = Math.max(MODEL_MENU_MARGIN, windowWidth - menuWidth - MODEL_MENU_MARGIN);
  const left = clamp(preferredLeft, MODEL_MENU_MARGIN, maxLeft);

  // The menu never exceeds the window; the list scrolls so the final item
  // stays reachable across compact screens and system navigation insets.
  const maxHeight = Math.max(
    120,
    Math.min(menuHeight, windowHeight - top - MODEL_MENU_MARGIN),
  );
  return { left, top, maxHeight };
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

interface SessionModelSheetProps {
  visible: boolean;
  loading: boolean;
  activating: boolean;
  error?: ProviderError | string | null;
  selection?: ProviderSessionSelection | null;
  /** Models of this Session's bound connection only (never other connections). */
  choices: ProviderModelChoice[];
  chrome: TerminalThemeChrome;
  /** On-screen anchor (the model control); null falls back to a top-right spot. */
  anchor?: MenuAnchorLayout | null;
  onRetry(): void;
  onClose(): void;
  onActivate(choice: SessionModelChoice): void;
}

/**
 * Compact current-Session model menu, anchored to the model control (an
 * upward-opening popover, matching common AI chat clients). Lists only the
 * models of the bound connection with the current selection checked; loading,
 * error, and retry appear only when genuinely needed. Provider configuration
 * stays in Settings — this surface never lists connections, shows Base URLs,
 * or opens Provider Settings.
 */
export function SessionModelSheet({
  visible,
  loading,
  activating,
  error,
  selection,
  choices,
  chrome,
  anchor = null,
  onRetry,
  onClose,
  onActivate,
}: SessionModelSheetProps) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const { width: windowWidth, height: windowHeight } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const errorMessage =
    typeof error === "string" ? error : error?.message ?? null;
  const refreshable =
    typeof error === "object" && error != null ? error.refreshable : true;
  const menuHeight = Math.min(
    MODEL_MENU_MAX_HEIGHT,
    Math.max(220, 64 + choices.length * 44 + (activating ? 40 : 0)),
  );
  const position = buildModelMenuPosition({
    anchor,
    windowWidth,
    windowHeight,
    menuWidth: MODEL_MENU_WIDTH,
    menuHeight,
  });
  const listMaxHeight = Math.max(
    120,
    position.maxHeight - 64 - (insets.bottom > 0 ? insets.bottom + 8 : 12),
  );

  return (
    <Modal
      visible={visible}
      transparent
      animationType="none"
      onRequestClose={onClose}
    >
      <View style={styles.root}>
        <Pressable
          style={styles.backdrop}
          accessibilityRole="button"
          accessibilityLabel="Close model menu"
          onPress={onClose}
        />
        <View
          style={[
            styles.menu,
            {
              left: position.left,
              top: position.top,
              width: MODEL_MENU_WIDTH,
              maxHeight: position.maxHeight,
              backgroundColor: chrome.surface,
              borderColor: chrome.border,
              shadowColor: chrome.overlay,
            },
          ]}
          accessibilityViewIsModal
        >
          <View style={styles.header}>
            <Text
              style={[styles.title, { color: chrome.text }]}
              accessibilityRole="header"
              numberOfLines={1}
            >
              Model
            </Text>
            <Pressable
              accessibilityLabel="Close model"
              accessibilityRole="button"
              style={[styles.close, { backgroundColor: chrome.surfaceMuted }]}
              onPress={onClose}
            >
              <Ionicons name="close" size={18} color={chrome.textSubtle} />
            </Pressable>
          </View>

          {loading && !selection ? (
            <View style={styles.center}>
              <ActivityIndicator color={chrome.accent} />
            </View>
          ) : null}

          {errorMessage && choices.length === 0 ? (
            <View style={styles.stateBlock}>
              <Text style={[styles.stateBody, { color: chrome.textMuted }]}>
                {errorMessage}
              </Text>
              {refreshable ? (
                <Pressable onPress={onRetry} accessibilityRole="button">
                  <Text style={[styles.link, { color: chrome.accent }]}>
                    Retry
                  </Text>
                </Pressable>
              ) : null}
            </View>
          ) : (
            <ScrollView
              style={{ maxHeight: listMaxHeight }}
              contentContainerStyle={[
                styles.body,
                { paddingBottom: Math.max(insets.bottom, 12) + 8 },
              ]}
              showsVerticalScrollIndicator={false}
            >
              {choices.map((choice) => {
                const selected = choice.current;
                const disabled = activating || choice.disabled || selected;
                return (
                  <Pressable
                    key={`${choice.connection.id}:${choice.model.id}`}
                    style={[
                      styles.modelRow,
                      {
                        backgroundColor: selected
                          ? chrome.accentSoft
                          : chrome.surfaceMuted,
                        opacity: disabled && !selected ? 0.55 : 1,
                      },
                    ]}
                    disabled={disabled}
                    accessibilityRole="button"
                    accessibilityState={{
                      disabled,
                      selected,
                    }}
                    accessibilityLabel={`Use ${choice.model.id}`}
                    onPress={() =>
                      onActivate({
                        connectionId: choice.connection.id,
                        modelId: choice.model.id,
                      })
                    }
                  >
                    <Text style={[styles.modelText, { color: chrome.text }]}>
                      {choice.model.id}
                    </Text>
                    {selected ? (
                      <Ionicons
                        name="checkmark"
                        size={16}
                        color={chrome.accent}
                      />
                    ) : null}
                  </Pressable>
                );
              })}

              {activating ? (
                <View style={styles.activatingRow}>
                  <ActivityIndicator size="small" color={chrome.accent} />
                  <Text
                    style={[styles.activatingText, { color: chrome.textMuted }]}
                  >
                    Switching…
                  </Text>
                </View>
              ) : null}

              {errorMessage && choices.length > 0 ? (
                <View style={styles.stateRow}>
                  <Text style={[styles.stateBody, { color: chrome.textMuted }]}>
                    {errorMessage}
                  </Text>
                  {refreshable ? (
                    <Pressable onPress={onRetry} accessibilityRole="button">
                      <Text style={[styles.link, { color: chrome.accent }]}>
                        Retry
                      </Text>
                    </Pressable>
                  ) : null}
                </View>
              ) : null}

              {!activating && !errorMessage && choices.length === 0 ? (
                <Text style={[styles.stateBody, { color: chrome.textMuted }]}>
                  No models discovered for this Session yet.
                </Text>
              ) : null}
            </ScrollView>
          )}
        </View>
      </View>
    </Modal>
  );
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    root: {
      flex: 1,
    },
    backdrop: {
      ...StyleSheet.absoluteFill,
      backgroundColor: "transparent",
    },
    menu: {
      position: "absolute",
      borderRadius: 14,
      borderWidth: StyleSheet.hairlineWidth,
      shadowOpacity: 0.2,
      shadowRadius: 10,
      elevation: 8,
      paddingBottom: 4,
    },
    header: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 14,
      paddingTop: 12,
      paddingBottom: 6,
      gap: 10,
    },
    title: { ...TypeScale.title, flex: 1 },
    close: {
      width: 30,
      height: 30,
      borderRadius: 15,
      alignItems: "center",
      justifyContent: "center",
    },
    center: { paddingVertical: 32, alignItems: "center" },
    stateBlock: { paddingHorizontal: 14, gap: 8, paddingBottom: 12 },
    stateRow: { gap: 4, paddingTop: 4, paddingHorizontal: 14 },
    stateBody: { ...TypeScale.body },
    link: { ...TypeScale.body, fontWeight: "600" },
    body: { paddingHorizontal: 14, gap: 6 },
    modelRow: {
      borderRadius: 10,
      paddingHorizontal: 12,
      paddingVertical: 10,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 8,
    },
    modelText: { ...TypeScale.body, flexShrink: 1 },
    activatingRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
      paddingVertical: 6,
      paddingHorizontal: 14,
    },
    activatingText: { ...TypeScale.body },
  });
}
