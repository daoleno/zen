import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView, useSafeAreaInsets } from "react-native-safe-area-context";
import {
  Radii,
  TypeScale,
  UiTextMetrics,
  useAppColors,
} from "../../constants/tokens";
import type { ProviderError } from "../../services/providers";
import type {
  ProviderConnection,
  ProviderPreset,
  ProvidersSnapshot,
} from "../../services/providers";
import {
  defaultClientsForConnection,
  futureDefaultRows,
} from "../../services/providers";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";
import { RisingSheet } from "../ui/RisingSheet";
import {
  providerEditorAfterSave,
  providerEditorCanSave,
  providerEditorSessionKey,
  providerEditorShouldResetFields,
  type ProviderSaveOutcome,
  type ProvidersEditorState,
} from "./providersPresentationModel";

export type {
  ProviderSaveOutcome,
  ProvidersEditorState,
} from "./providersPresentationModel";

export interface ProvidersPresentationProps {
  catalog: ProvidersSnapshot | null;
  loading: boolean;
  refreshing: boolean;
  error: ProviderError | null;
  offline: boolean;
  unavailable: boolean;
  currentServerAvailable: boolean;
  durabilityWarning?: string | null;
  requiresRefreshBeforeMutation?: boolean;
  editor: ProvidersEditorState;
  mutating: boolean;
  onRefresh(): void;
  onOpenSettings(): void;
  onOpenEditor(editor: NonNullable<ProvidersEditorState>): void;
  onCloseEditor(): void;
  onClearCredential(connection: ProviderConnection): void;
  onDelete(connection: ProviderConnection): void;
  onSetDefault(client: string, connection: ProviderConnection): void;
  onDiscover(connection: ProviderConnection): void;
  onSaveCurated(
    preset: ProviderPreset,
    apiKey: string,
  ): Promise<ProviderSaveOutcome> | ProviderSaveOutcome;
  onSaveCustom(input: {
    name: string;
    baseUrl: string;
    apiKey: string;
  }): Promise<ProviderSaveOutcome> | ProviderSaveOutcome;
  onSaveCredential(
    connection: ProviderConnection,
    apiKey: string,
  ): Promise<ProviderSaveOutcome> | ProviderSaveOutcome;
  /**
   * Demo/screenshot affordance only: focus the API key field when the editor
   * opens so the keyboard state can be captured deterministically.
   */
  apiKeyAutoFocus?: boolean;
}

export function ProvidersPresentation({
  catalog,
  loading,
  refreshing,
  error,
  offline,
  unavailable,
  currentServerAvailable,
  durabilityWarning,
  requiresRefreshBeforeMutation,
  editor,
  mutating,
  onRefresh,
  onOpenSettings,
  onOpenEditor,
  onCloseEditor,
  onClearCredential,
  onDelete,
  onSetDefault,
  onDiscover,
  onSaveCurated,
  onSaveCustom,
  onSaveCredential,
  apiKeyAutoFocus,
}: ProvidersPresentationProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const connections = catalog?.connections ?? [];
  const presets = catalog?.presets ?? [];
  const defaultRows = futureDefaultRows(catalog);
  const connectedPresetIds = new Set(
    connections
      .map((connection) => connection.preset_id)
      .filter((id): id is string => Boolean(id)),
  );
  const curatedPresets = presets.filter(
    (preset) => preset.advanced !== true && !connectedPresetIds.has(preset.id),
  );
  const canMutate =
    currentServerAvailable && !offline && !unavailable && Boolean(catalog);

  return (
    <SafeAreaView style={styles.safe} edges={["bottom"]}>
      <ScrollView
        contentContainerStyle={styles.content}
        keyboardShouldPersistTaps="handled"
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={onRefresh}
            tintColor={colors.accent}
          />
        }
      >
        {!currentServerAvailable || offline ? (
          <View style={styles.stateBlock}>
            <Text style={styles.stateTitle}>Offline</Text>
            <Text style={styles.stateBody}>
              Connect the current server to manage Providers.
            </Text>
            <Pressable onPress={onOpenSettings} accessibilityRole="button">
              <Text style={styles.link}>Open Settings</Text>
            </Pressable>
          </View>
        ) : null}

        {unavailable ? (
          <View style={styles.stateBlock}>
            <Text style={styles.stateTitle}>Unavailable</Text>
            <Text style={styles.stateBody}>
              Providers are not available on this daemon.
            </Text>
          </View>
        ) : null}

        {error && !unavailable && currentServerAvailable && !offline ? (
          <View style={styles.stateBlock}>
            <Text style={styles.stateTitle}>
              {error.refreshable ? "Refresh required" : "Could not load"}
            </Text>
            <Text style={styles.stateBody}>{error.message}</Text>
            {error.refreshable ? (
              <Pressable onPress={onRefresh} accessibilityRole="button">
                <Text style={styles.link}>Refresh</Text>
              </Pressable>
            ) : null}
          </View>
        ) : null}

        {durabilityWarning ? (
          <View style={[styles.notice, { borderLeftColor: colors.warning }]}>
            <Text style={styles.noticeText}>{durabilityWarning}</Text>
          </View>
        ) : null}

        {requiresRefreshBeforeMutation ? (
          <View style={[styles.notice, { borderLeftColor: colors.accent }]}>
            <Text style={styles.noticeText}>
              Writes are locked until a successful refresh completes.
            </Text>
            <Pressable onPress={onRefresh} accessibilityRole="button">
              <Text style={styles.link}>Refresh</Text>
            </Pressable>
          </View>
        ) : null}

        {loading && !catalog ? (
          <View style={styles.center}>
            <ActivityIndicator color={colors.accent} />
          </View>
        ) : null}

        {catalog && defaultRows.length > 0 && connections.length > 0 ? (
          <>
            <Text style={styles.sectionLabel} accessibilityRole="header">
              Defaults
            </Text>
            <View
              style={styles.group}
              accessibilityLabel="Future defaults"
              accessibilityRole="radiogroup"
            >
              {defaultRows.map((row) => (
                <View
                  key={row.client}
                  style={[
                    styles.groupRow,
                    row !== defaultRows[defaultRows.length - 1] &&
                      styles.groupRowBorder,
                  ]}
                >
                  <View style={styles.rowCopy}>
                    <Text style={styles.rowTitle}>{row.label} Sessions</Text>
                    <Text style={styles.rowSubtitle}>
                      {row.currentConnectionName
                        ? `Default · ${row.currentConnectionName}`
                        : row.options.length > 0
                          ? "No default yet"
                          : "Add a ready Provider to set a default"}
                    </Text>
                  </View>
                  {row.options.length > 0 ? (
                    <View style={styles.chipRow}>
                      {row.options.map((option) => {
                        const connection = connections.find(
                          (item) => item.id === option.connectionId,
                        );
                        if (!connection) return null;
                        return (
                          <AnimatedPressable
                            key={`${row.client}-${option.connectionId}`}
                            style={[
                              styles.chip,
                              option.selected && {
                                backgroundColor: colors.accentSoft,
                              },
                            ]}
                            onPress={() =>
                              onSetDefault(row.client, connection)
                            }
                            disabled={
                              mutating ||
                              requiresRefreshBeforeMutation ||
                              option.selected
                            }
                            accessibilityRole="radio"
                            accessibilityState={{
                              checked: option.selected,
                              disabled:
                                option.selected ||
                                mutating ||
                                requiresRefreshBeforeMutation,
                            }}
                            accessibilityLabel={`${row.label} default ${option.connectionName}`}
                          >
                            <Text
                              style={[
                                styles.chipText,
                                option.selected && {
                                  color: colors.accentStrong,
                                },
                              ]}
                            >
                              {option.connectionName}
                            </Text>
                          </AnimatedPressable>
                        );
                      })}
                    </View>
                  ) : null}
                </View>
              ))}
            </View>
          </>
        ) : null}

        {catalog ? (
          <>
            {connections.length > 0 ? (
              <>
                <Text style={styles.sectionLabel} accessibilityRole="header">
                  Providers
                </Text>
                <View style={styles.group}>
                  {connections.map((connection, index) => {
                    const defaultClients = defaultClientsForConnection(
                      catalog,
                      connection.id,
                    );
                    return (
                      <ProviderConnectionRow
                        key={connection.id}
                        connection={connection}
                        isLast={index === connections.length - 1}
                        defaultLabel={
                          connection.advanced
                            ? connection.base_url ?? "Custom endpoint"
                            : defaultClients.length > 0
                              ? "Default"
                              : "Not a default"
                        }
                        mutating={mutating}
                        requiresRefreshBeforeMutation={requiresRefreshBeforeMutation}
                        onAddApiKey={onOpenEditor}
                        onClearCredential={onClearCredential}
                        onDelete={onDelete}
                        onDiscover={onDiscover}
                      />
                    );
                  })}
                </View>
              </>
            ) : null}

            {canMutate ? (
              <>
                <Text style={styles.sectionLabel} accessibilityRole="header">
                  Connect a service
                </Text>
                <View style={styles.group}>
                  {curatedPresets.map((preset) => (
                    <PresetConnectRow
                      key={preset.id}
                      preset={preset}
                      isLast={false}
                      disabled={mutating || requiresRefreshBeforeMutation}
                      onConnect={() =>
                        onOpenEditor({ kind: "preset", presetId: preset.id })
                      }
                    />
                  ))}
                  <CustomEndpointRow
                    isLast
                    disabled={mutating || requiresRefreshBeforeMutation}
                    onPress={() => onOpenEditor({ kind: "custom" })}
                  />
                </View>
              </>
            ) : null}

            {connections.length === 0 && presets.length === 0 ? (
              <View style={styles.group}>
                <View style={styles.emptyCard}>
                  <Text style={styles.emptyTitle}>
                    No services connected yet
                  </Text>
                  <Text style={styles.emptyHint}>
                    Connect a service below, or use a custom endpoint.
                  </Text>
                </View>
              </View>
            ) : null}
          </>
        ) : null}
      </ScrollView>

      <ProviderEditorSheet
        editor={editor}
        presets={catalog?.presets ?? []}
        mutating={mutating}
        apiKeyAutoFocus={apiKeyAutoFocus}
        onClose={onCloseEditor}
        onSaveCurated={onSaveCurated}
        onSaveCustom={onSaveCustom}
        onSaveCredential={onSaveCredential}
      />
    </SafeAreaView>
  );
}

function PresetConnectRow({
  preset,
  isLast,
  disabled,
  onConnect,
}: {
  preset: ProviderPreset;
  isLast: boolean;
  disabled?: boolean;
  onConnect(): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <AnimatedPressable
      style={[
        styles.groupRow,
        !isLast && styles.groupRowBorder,
        styles.presetRow,
      ]}
      preset="press"
      scale={0.99}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityLabel={`Connect ${preset.label}`}
      accessibilityHint={`Connect ${preset.label} with an API key`}
      accessibilityState={{ disabled }}
      onPress={onConnect}
    >
      <View style={styles.rowCopy}>
        <Text style={styles.rowTitle} numberOfLines={1}>
          {preset.label}
        </Text>
      </View>
      <View style={styles.connectPill}>
        <Text style={styles.connectPillText}>Connect</Text>
      </View>
    </AnimatedPressable>
  );
}

function CustomEndpointRow({
  isLast,
  disabled,
  onPress,
}: {
  isLast: boolean;
  disabled?: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <AnimatedPressable
      style={[
        styles.groupRow,
        styles.customRow,
        !isLast && styles.groupRowBorder,
      ]}
      preset="press"
      scale={0.99}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityLabel="Use custom endpoint"
      accessibilityHint="Set up a service with a display name, endpoint URL, and API key"
      accessibilityState={{ disabled }}
      onPress={onPress}
    >
      <Ionicons
        name="add-circle-outline"
        size={20}
        color={colors.textSecondary}
      />
      <View style={styles.rowCopy}>
        <Text style={styles.customTitle}>Use custom endpoint</Text>
        <Text style={styles.customSubtitle}>
          Display name, endpoint URL, and API key
        </Text>
      </View>
      <Ionicons
        name="chevron-forward"
        size={18}
        color={colors.textTertiary}
      />
    </AnimatedPressable>
  );
}

function ProviderConnectionRow({
  connection,
  isLast,
  defaultLabel,
  mutating,
  requiresRefreshBeforeMutation,
  onAddApiKey,
  onClearCredential,
  onDelete,
  onDiscover,
}: {
  connection: ProviderConnection;
  isLast: boolean;
  defaultLabel: string;
  mutating: boolean;
  requiresRefreshBeforeMutation?: boolean;
  onAddApiKey(
    editor: NonNullable<ProvidersEditorState>,
  ): void;
  onClearCredential(connection: ProviderConnection): void;
  onDelete(connection: ProviderConnection): void;
  onDiscover(connection: ProviderConnection): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const [expanded, setExpanded] = useState(false);
  const ready = connection.credential_ready;
  const writeLocked = mutating || requiresRefreshBeforeMutation;

  return (
    <View
      style={[
        styles.groupRow,
        !isLast && styles.groupRowBorder,
        styles.providerRow,
      ]}
      accessibilityLabel={`Provider ${connection.name}`}
    >
      <AnimatedPressable
        style={styles.providerHeader}
        preset="press"
        scale={0.99}
        accessibilityRole="button"
        accessibilityLabel={`${connection.name}, ${
          ready ? "Connected" : "Needs API key"
        }`}
        accessibilityHint={
          expanded ? "Hide Provider actions" : "Show Provider actions"
        }
        accessibilityState={{ expanded }}
        onPress={() => setExpanded((open) => !open)}
      >
        <View style={styles.rowCopy}>
          <Text style={styles.rowTitle} numberOfLines={1}>
            {connection.name}
          </Text>
          <Text style={styles.rowSubtitle} numberOfLines={1}>
            {defaultLabel}
          </Text>
        </View>
        <View
          style={[
            styles.statusPill,
            {
              backgroundColor: ready ? colors.successSoft : colors.warningSoft,
            },
          ]}
        >
          <View
            style={[
              styles.statusDot,
              { backgroundColor: ready ? colors.success : colors.warning },
            ]}
          />
          <Text
            style={[
              styles.statusText,
              { color: ready ? colors.success : colors.warning },
            ]}
          >
            {ready ? "Connected" : "Needs API key"}
          </Text>
        </View>
        <Ionicons
          name={expanded ? "chevron-up" : "chevron-down"}
          size={16}
          color={colors.textTertiary}
        />
      </AnimatedPressable>

      {expanded ? (
        <View style={styles.providerActions}>
          {!ready ? (
            <AnimatedPressable
              style={[styles.actionBtn, styles.actionBtnPrimary]}
              onPress={() => onAddApiKey({ kind: "credential", connection })}
              disabled={writeLocked}
              accessibilityRole="button"
              accessibilityLabel="Add API key"
            >
              <Text style={[styles.actionBtnText, styles.actionBtnPrimaryText]}>
                Add API key
              </Text>
            </AnimatedPressable>
          ) : (
            <>
              <AnimatedPressable
                style={[styles.actionBtn, styles.actionBtnPrimary]}
                onPress={() =>
                  onAddApiKey({ kind: "credential", connection })
                }
                disabled={writeLocked}
                accessibilityRole="button"
                accessibilityLabel="Replace API key"
              >
                <Text
                  style={[styles.actionBtnText, styles.actionBtnPrimaryText]}
                >
                  Replace key
                </Text>
              </AnimatedPressable>
              <AnimatedPressable
                style={styles.actionBtn}
                onPress={() => onDiscover(connection)}
                disabled={writeLocked}
                accessibilityRole="button"
                accessibilityLabel="Refresh models"
              >
                <Text style={styles.actionBtnText}>Refresh models</Text>
              </AnimatedPressable>
              <AnimatedPressable
                style={[styles.actionBtn, styles.actionBtnDanger]}
                onPress={() => onClearCredential(connection)}
                disabled={writeLocked}
                accessibilityRole="button"
                accessibilityLabel="Clear API key"
              >
                <Text
                  style={[styles.actionBtnText, styles.actionBtnDangerText]}
                >
                  Clear key
                </Text>
              </AnimatedPressable>
            </>
          )}
          <AnimatedPressable
            style={[styles.actionBtn, styles.actionBtnDanger]}
            onPress={() => onDelete(connection)}
            disabled={writeLocked}
            accessibilityRole="button"
            accessibilityLabel="Delete provider"
          >
            <Text style={[styles.actionBtnText, styles.actionBtnDangerText]}>
              Delete
            </Text>
          </AnimatedPressable>
        </View>
      ) : null}
    </View>
  );
}

interface ProviderEditorSheetProps {
  editor: ProvidersEditorState;
  presets: ProviderPreset[];
  mutating: boolean;
  apiKeyAutoFocus?: boolean;
  onClose(): void;
  onSaveCurated(
    preset: ProviderPreset,
    apiKey: string,
  ): Promise<ProviderSaveOutcome> | ProviderSaveOutcome;
  onSaveCustom(input: {
    name: string;
    baseUrl: string;
    apiKey: string;
  }): Promise<ProviderSaveOutcome> | ProviderSaveOutcome;
  onSaveCredential(
    connection: ProviderConnection,
    apiKey: string,
  ): Promise<ProviderSaveOutcome> | ProviderSaveOutcome;
}

function ProviderEditorSheet({
  editor,
  presets,
  mutating,
  apiKeyAutoFocus,
  onClose,
  onSaveCurated,
  onSaveCustom,
  onSaveCredential,
}: ProviderEditorSheetProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const insets = useSafeAreaInsets();
  const [apiKey, setApiKey] = useState("");
  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("");

  const sessionKey = providerEditorSessionKey(editor);
  const prevSessionKeyRef = useRef(sessionKey);

  useEffect(() => {
    const previous = prevSessionKeyRef.current;
    prevSessionKeyRef.current = sessionKey;
    if (providerEditorShouldResetFields(previous, sessionKey)) {
      setApiKey("");
      setName("");
      setBaseUrl("");
    }
  }, [sessionKey]);

  const resetFields = () => {
    setApiKey("");
    setName("");
    setBaseUrl("");
  };

  const handleClose = () => {
    resetFields();
    onClose();
  };

  const connection =
    editor?.kind === "credential" ? editor.connection : null;
  const presetTarget =
    editor?.kind === "preset"
      ? (presets.find((preset) => preset.id === editor.presetId) ?? null)
      : null;
  const customMode = editor?.kind === "custom";
  const canSave = providerEditorCanSave({
    mutating,
    apiKey,
    credentialMode: connection != null,
    presetMode: presetTarget != null && presetTarget.advanced !== true,
    customMode,
    name,
    baseUrl,
  });
  const title = connection
    ? connection.credential_ready
      ? "Replace API key"
      : editor?.kind === "credential" && editor.retry
        ? "Retry API key"
        : "Add API key"
    : presetTarget
      ? `Connect ${presetTarget.label}`
      : customMode
        ? "Custom endpoint"
        : "Connect";

  const handleSave = async () => {
    if (!canSave) return;
    const outcome = connection
      ? await onSaveCredential(connection, apiKey)
      : presetTarget
        ? await onSaveCurated(presetTarget, apiKey)
        : customMode
          ? await onSaveCustom({
              name: name.trim(),
              baseUrl: baseUrl.trim(),
              apiKey,
            })
          : null;
    if (outcome?.status === "saved") {
      resetFields();
    }
  };

  return (
    <RisingSheet
      visible={editor !== null}
      onClose={handleClose}
      align="bottom"
      avoidKeyboard
      cardStyle={[
        styles.editorCard,
        { paddingBottom: Math.max(insets.bottom, 16) },
      ]}
    >
      <ScrollView
        contentContainerStyle={styles.editorContent}
        keyboardShouldPersistTaps="handled"
        showsVerticalScrollIndicator={false}
      >
        <View style={styles.editorHeader}>
          <Text style={styles.editorTitle} accessibilityRole="header">
            {title}
          </Text>
          <Pressable
            accessibilityLabel="Close"
            accessibilityRole="button"
            hitSlop={8}
            style={styles.editorClose}
            onPress={handleClose}
          >
            <Ionicons name="close" size={22} color={colors.textSecondary} />
          </Pressable>
        </View>

        {connection ? (
          <>
            <Text style={styles.editorProviderName}>{connection.name}</Text>
            {editor?.kind === "credential" && editor.retry ? (
              <Text style={styles.editorRetryHint}>
                The API key wasn't saved. Try again with the same key.
              </Text>
            ) : null}
          </>
        ) : null}

        {customMode && !connection ? (
          <>
            <Text style={styles.fieldLabel}>Display name</Text>
            <MobileSingleLineInput
              value={name}
              onChangeText={setName}
              editable={!mutating}
              placeholder="My gateway"
              placeholderTextColor={colors.textTertiary}
              accessibilityLabel="Display name"
              autoCapitalize="words"
              autoCorrect={false}
              containerStyle={styles.field}
            />
            <Text style={styles.fieldLabel}>Endpoint URL</Text>
            <MobileSingleLineInput
              value={baseUrl}
              onChangeText={setBaseUrl}
              editable={!mutating}
              placeholder="https://…"
              placeholderTextColor={colors.textTertiary}
              accessibilityLabel="Endpoint URL"
              autoCapitalize="none"
              autoCorrect={false}
              autoComplete="off"
              textContentType="URL"
              keyboardType="url"
              containerStyle={styles.field}
            />
          </>
        ) : null}

        <Text style={styles.fieldLabel}>API Key</Text>
        <MobileSingleLineInput
          value={apiKey}
          onChangeText={setApiKey}
          editable={!mutating}
          autoFocus={apiKeyAutoFocus && !mutating}
          placeholder="Paste API key"
          placeholderTextColor={colors.textTertiary}
          accessibilityLabel="API Key"
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          autoComplete="off"
          textContentType="none"
          containerStyle={styles.field}
        />

        <AnimatedPressable
          style={[styles.saveBtn, !canSave && styles.saveBtnDisabled]}
          preset="press"
          scale={0.98}
          disabled={!canSave}
          accessibilityRole="button"
          accessibilityLabel={connection ? "Save" : "Connect"}
          accessibilityState={{ disabled: !canSave, busy: mutating }}
          onPress={() => void handleSave()}
        >
          <Text style={styles.saveBtnText}>
            {mutating
              ? connection
                ? "Saving…"
                : "Connecting…"
              : connection
                ? "Save"
                : "Connect"}
          </Text>
        </AnimatedPressable>
      </ScrollView>
    </RisingSheet>
  );
}

function createStyles(colors: ReturnType<typeof useAppColors>) {
  return StyleSheet.create({
    safe: { flex: 1, backgroundColor: colors.bgPrimary },
    content: { paddingHorizontal: 16, paddingTop: 16, paddingBottom: 32, gap: 8 },
    center: { paddingVertical: 48, alignItems: "center" },
    stateBlock: {
      padding: 14,
      borderRadius: Radii.xs,
      backgroundColor: colors.bgSurface,
      gap: 6,
    },
    stateTitle: {
      ...UiTextMetrics,
      ...TypeScale.title,
      color: colors.textPrimary,
    },
    stateBody: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.textSecondary,
    },
    link: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.accent,
      fontWeight: "600",
    },
    notice: {
      padding: 12,
      borderRadius: Radii.xs,
      borderLeftWidth: 3,
      backgroundColor: colors.bgSurface,
      gap: 6,
    },
    noticeText: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textPrimary,
    },
    sectionLabel: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textSecondary,
      marginTop: 16,
      marginBottom: 8,
      marginHorizontal: 4,
    },
    group: {
      overflow: "hidden",
      borderRadius: Radii.xs,
      backgroundColor: colors.bgSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    groupRow: {
      backgroundColor: colors.bgSurface,
    },
    groupRowBorder: {
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    rowCopy: { flex: 1, minWidth: 0 },
    rowTitle: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.textPrimary,
    },
    rowSubtitle: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textTertiary,
      marginTop: 2,
    },
    chipRow: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 8,
      marginTop: 8,
    },
    chip: {
      paddingHorizontal: 12,
      paddingVertical: 7,
      borderRadius: Radii.pill,
      backgroundColor: colors.surfaceSubtle,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
    },
    chipText: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textPrimary,
      fontWeight: "600",
    },
    providerRow: {
      paddingHorizontal: 14,
    },
    providerHeader: {
      minHeight: 64,
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
      paddingVertical: 10,
    },
    statusPill: {
      minHeight: 26,
      paddingHorizontal: 9,
      borderRadius: 13,
      flexDirection: "row",
      alignItems: "center",
      gap: 5,
    },
    statusDot: { width: 6, height: 6, borderRadius: 3 },
    statusText: {
      ...UiTextMetrics,
      ...TypeScale.micro,
    },
    providerActions: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 8,
      paddingVertical: 12,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    actionBtn: {
      minHeight: 44,
      minWidth: 92,
      flexGrow: 1,
      paddingHorizontal: 14,
      borderRadius: Radii.xs,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfacePressed,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    actionBtnPrimary: {
      backgroundColor: colors.accentSoft,
      borderColor: colors.accent,
    },
    actionBtnText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textPrimary,
    },
    actionBtnPrimaryText: {
      color: colors.accentStrong,
    },
    actionBtnDanger: {
      backgroundColor: colors.dangerSoft,
      borderColor: colors.dangerText,
    },
    actionBtnDangerText: {
      color: colors.dangerText,
    },
    emptyCard: {
      paddingHorizontal: 16,
      paddingVertical: 20,
      gap: 4,
      backgroundColor: colors.bgSurface,
    },
    emptyTitle: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textPrimary,
    },
    emptyHint: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textTertiary,
    },
    presetRow: {
      minHeight: 56,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingHorizontal: 14,
      paddingVertical: 8,
    },
    connectPill: {
      minHeight: 32,
      paddingHorizontal: 16,
      borderRadius: Radii.pill,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.accentSoft,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.accent,
    },
    connectPillText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.accentStrong,
      fontWeight: "600",
    },
    customRow: {
      minHeight: 60,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingHorizontal: 14,
      paddingVertical: 8,
      backgroundColor: colors.surfaceSubtle,
    },
    customTitle: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textSecondary,
    },
    customSubtitle: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textTertiary,
      marginTop: 1,
    },
    editorCard: {
      backgroundColor: colors.bgSurface,
      borderTopLeftRadius: Radii.sm,
      borderTopRightRadius: Radii.sm,
      overflow: "hidden",
      maxHeight: 560,
    },
    editorContent: { paddingHorizontal: 16, paddingTop: 8, gap: 8 },
    editorHeader: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      marginBottom: 4,
    },
    editorTitle: {
      ...UiTextMetrics,
      ...TypeScale.title,
      color: colors.textPrimary,
    },
    editorClose: {
      width: 36,
      height: 36,
      borderRadius: 18,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfaceSubtle,
    },
    editorProviderName: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.textSecondary,
      marginBottom: 4,
    },
    editorRetryHint: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.warning,
      marginBottom: 4,
    },
    fieldLabel: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textSecondary,
      marginTop: 8,
      marginBottom: 2,
      marginHorizontal: 2,
    },
    field: {
      borderRadius: Radii.xs,
      backgroundColor: colors.inputBackground,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
    },
    saveBtn: {
      minHeight: 50,
      marginTop: 12,
      borderRadius: Radii.xs,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.accent,
    },
    saveBtnDisabled: {
      opacity: 0.45,
    },
    saveBtnText: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.textOnAccent,
      fontWeight: "700",
    },
  });
}
