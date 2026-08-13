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
import { Claude, Codex } from "@lobehub/icons-rn";
import { SafeAreaView, useSafeAreaInsets } from "react-native-safe-area-context";
import {
  Radii,
  TypeScale,
  UiTextMetrics,
  useAppColors,
} from "../../constants/tokens";
import type { ProviderError } from "../../services/providers";
import type {
  ProviderClient,
  ProviderConnection,
  ProviderConnectionTestResult,
  ProvidersSnapshot,
} from "../../services/providers";
import {
  boundModelForConnection,
  connectionRequiresModelSelection,
  connectionsForClient,
  modelSupportChoices,
  providerClientLabel,
} from "../../services/providers";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";
import { RisingSheet } from "../ui/RisingSheet";
import {
  providerEditorCanSave,
  providerEditorSessionKey,
  providerEditorShouldResetFields,
  type ModelSyncPickerState,
  type ProviderSaveOutcome,
  type ProvidersEditorState,
} from "./providersPresentationModel";

export type {
  ModelSyncPickerState,
  ProviderSaveOutcome,
  ProvidersEditorState,
} from "./providersPresentationModel";

const CLIENTS: ProviderClient[] = ["codex", "claude"];

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
  onDelete(connection: ProviderConnection): void;
  onUseDirect(client: ProviderClient): void;
  onSetDefault(client: ProviderClient, connection: ProviderConnection): void;
  onDiscover(connection: ProviderConnection): void;
  modelPicker: ModelSyncPickerState | null;
  onCloseModelPicker(): void;
  onSelectModel(
    client: ProviderClient,
    connection: ProviderConnection,
    modelId: string,
  ): void;
  onTestConnection(input: {
    client: ProviderClient;
    baseUrl: string;
    apiKey: string;
  }): Promise<ProviderConnectionTestResult>;
  onSaveCustom(input: {
    client: ProviderClient;
    baseUrl: string;
    apiKey: string;
  }): Promise<ProviderSaveOutcome> | ProviderSaveOutcome;
  onSaveCredential(
    connection: ProviderConnection,
    apiKey: string,
  ): Promise<ProviderSaveOutcome> | ProviderSaveOutcome;
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
  onDelete,
  onUseDirect,
  onSetDefault,
  onDiscover,
  modelPicker,
  onCloseModelPicker,
  onSelectModel,
  onTestConnection,
  onSaveCustom,
  onSaveCredential,
  apiKeyAutoFocus,
}: ProvidersPresentationProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const canMutate =
    currentServerAvailable && !offline && !unavailable && Boolean(catalog);
  const writeLocked = mutating || Boolean(requiresRefreshBeforeMutation);

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
              Connect the current server to configure model access.
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
              Model connections are not available on this daemon.
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
              Changes are paused until this list is refreshed.
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

        {catalog ? (
          <>
            <View style={styles.intro}>
              <Text style={styles.introTitle}>Model connections</Text>
              <Text style={styles.introBody}>
                Configure each client separately. Official login stays direct;
                Zen only routes Sessions that use a custom endpoint.
              </Text>
            </View>
            {CLIENTS.map((client) => (
              <ClientConnectionCard
                key={client}
                client={client}
                catalog={catalog}
                disabled={!canMutate || writeLocked}
                onUseDirect={() => onUseDirect(client)}
                onSetDefault={(connection) => onSetDefault(client, connection)}
                onOpenEditor={onOpenEditor}
                onDelete={onDelete}
                onDiscover={onDiscover}
              />
            ))}
          </>
        ) : null}
      </ScrollView>

      <ProviderEditorSheet
        editor={editor}
        mutating={mutating}
        apiKeyAutoFocus={apiKeyAutoFocus}
        onClose={onCloseEditor}
        onTestConnection={onTestConnection}
        onSaveCustom={onSaveCustom}
        onSaveCredential={onSaveCredential}
      />

      {modelPicker && catalog ? (
        <ModelSyncSheet
          picker={modelPicker}
          catalog={catalog}
          mutating={mutating}
          disabled={!canMutate || writeLocked}
          onClose={onCloseModelPicker}
          onSelectModel={onSelectModel}
        />
      ) : null}
    </SafeAreaView>
  );
}

function ClientConnectionCard({
  client,
  catalog,
  disabled,
  onUseDirect,
  onSetDefault,
  onOpenEditor,
  onDelete,
  onDiscover,
}: {
  client: ProviderClient;
  catalog: ProvidersSnapshot;
  disabled: boolean;
  onUseDirect(): void;
  onSetDefault(connection: ProviderConnection): void;
  onOpenEditor(editor: NonNullable<ProvidersEditorState>): void;
  onDelete(connection: ProviderConnection): void;
  onDiscover(connection: ProviderConnection): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const connections = connectionsForClient(catalog, client);
  const selectedId = catalog.defaults[client]?.connection_id ?? "";
  const direct = selectedId === "";
  const label = providerClientLabel(client);

  return (
    <View style={styles.clientSection}>
      <View style={styles.clientHeader}>
        <View style={styles.clientIcon}>
          {client === "codex" ? (
            <Codex.Color size={22} />
          ) : (
            <Claude.Color size={22} />
          )}
        </View>
        <View style={styles.rowCopy}>
          <Text style={styles.clientTitle}>{label}</Text>
          <Text style={styles.clientSummary}>
            {direct ? "Official login · Direct" : "Custom endpoint"}
          </Text>
        </View>
      </View>

      <View style={styles.group} accessibilityRole="radiogroup">
        <ChoiceRow
          title="Official login"
          subtitle={`Use ${label}'s own account. No Zen routing.`}
          selected={direct}
          disabled={disabled || direct}
          onPress={onUseDirect}
          isLast={connections.length === 0}
        />
        {connections.map((connection, index) => {
          const boundModel = boundModelForConnection(
            catalog,
            client,
            connection.id,
          );
          const needsModel = connectionRequiresModelSelection(
            catalog,
            client,
            connection.id,
          );
          return (
            <ConnectionChoiceRow
              key={connection.id}
              connection={connection}
              selected={selectedId === connection.id}
              disabled={disabled}
              isLast={index === connections.length - 1}
              boundModel={boundModel}
              needsModel={needsModel}
              onSelect={() => onSetDefault(connection)}
              onOpenCredential={() =>
                onOpenEditor({ kind: "credential", connection })
              }
              onDelete={() => onDelete(connection)}
              onDiscover={() => onDiscover(connection)}
            />
          );
        })}
      </View>

      <AnimatedPressable
        style={styles.addEndpoint}
        preset="press"
        scale={0.99}
        disabled={disabled}
        accessibilityRole="button"
        accessibilityLabel={`Add ${label} endpoint`}
        accessibilityState={{ disabled }}
        onPress={() => onOpenEditor({ kind: "custom", client })}
      >
        <Ionicons name="add" size={18} color={colors.accentStrong} />
        <Text style={styles.addEndpointText}>Add custom endpoint</Text>
      </AnimatedPressable>
    </View>
  );
}

function ChoiceRow({
  title,
  subtitle,
  selected,
  disabled,
  isLast,
  onPress,
}: {
  title: string;
  subtitle: string;
  selected: boolean;
  disabled: boolean;
  isLast: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <AnimatedPressable
      style={[styles.choiceRow, !isLast && styles.groupRowBorder]}
      preset="press"
      scale={0.995}
      disabled={disabled}
      accessibilityRole="radio"
      accessibilityState={{ checked: selected, disabled }}
      onPress={onPress}
    >
      <View style={styles.radioOuter}>
        {selected ? <View style={styles.radioInner} /> : null}
      </View>
      <View style={styles.rowCopy}>
        <Text style={styles.rowTitle}>{title}</Text>
        <Text style={styles.rowSubtitle}>{subtitle}</Text>
      </View>
    </AnimatedPressable>
  );
}

function ConnectionChoiceRow({
  connection,
  selected,
  disabled,
  isLast,
  boundModel,
  needsModel,
  onSelect,
  onOpenCredential,
  onDelete,
  onDiscover,
}: {
  connection: ProviderConnection;
  selected: boolean;
  disabled: boolean;
  isLast: boolean;
  /** Model bound as this client default, when this connection is the default. */
  boundModel: string | null;
  /** Default connection with no bound model: new Sessions would fail closed. */
  needsModel: boolean;
  onSelect(): void;
  onOpenCredential(): void;
  onDelete(): void;
  onDiscover(): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const [expanded, setExpanded] = useState(false);
  const ready = connection.credential_ready;
  const subtitle = connection.base_url ?? connection.name;

  return (
    <View style={!isLast ? styles.groupRowBorder : undefined}>
      <View style={styles.connectionRow}>
        <AnimatedPressable
          style={styles.connectionSelect}
          preset="press"
          scale={0.995}
          disabled={disabled}
          accessibilityRole="radio"
          accessibilityState={{ checked: selected, disabled }}
          accessibilityLabel={`${connection.name}, ${ready ? "connected" : "API key required"}${boundModel ? `, model ${boundModel}` : needsModel ? ", no model selected" : ""}`}
          onPress={ready ? onSelect : onOpenCredential}
        >
          <View style={styles.radioOuter}>
            {selected ? <View style={styles.radioInner} /> : null}
          </View>
          <View style={styles.rowCopy}>
            <Text style={styles.rowTitle} numberOfLines={1}>
              {connection.name}
            </Text>
            <Text style={styles.rowSubtitle} numberOfLines={1}>
              {subtitle}
            </Text>
            {boundModel ? (
              <Text style={styles.rowModel} numberOfLines={1}>
                Model · {boundModel}
              </Text>
            ) : needsModel ? (
              <Text style={styles.rowModelHint} numberOfLines={1}>
                No model selected · Sync models
              </Text>
            ) : null}
          </View>
          {!ready ? (
            <Text style={styles.keyRequired}>Add key</Text>
          ) : null}
        </AnimatedPressable>
        <Pressable
          style={styles.expandButton}
          hitSlop={6}
          accessibilityRole="button"
          accessibilityLabel={`${expanded ? "Hide" : "Show"} ${connection.name} actions`}
          onPress={() => setExpanded((value) => !value)}
        >
          <Ionicons
            name={expanded ? "chevron-up" : "ellipsis-horizontal"}
            size={18}
            color={colors.textTertiary}
          />
        </Pressable>
      </View>
      {expanded ? (
        <View style={styles.connectionActions}>
          {ready ? (
            <ActionButton label="Sync models" onPress={onDiscover} disabled={disabled} />
          ) : null}
          <ActionButton
            label={ready ? "Replace key" : "Add API key"}
            onPress={onOpenCredential}
            disabled={disabled}
            primary
          />
          <ActionButton label="Delete" onPress={onDelete} disabled={disabled} danger />
        </View>
      ) : null}
    </View>
  );
}

function ActionButton({
  label,
  onPress,
  disabled,
  primary,
  danger,
}: {
  label: string;
  onPress(): void;
  disabled: boolean;
  primary?: boolean;
  danger?: boolean;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <AnimatedPressable
      style={[
        styles.actionButton,
        primary && styles.actionButtonPrimary,
        danger && styles.actionButtonDanger,
      ]}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityState={{ disabled }}
      onPress={onPress}
    >
      <Text
        style={[
          styles.actionButtonText,
          primary && styles.actionButtonPrimaryText,
          danger && styles.actionButtonDangerText,
        ]}
      >
        {label}
      </Text>
    </AnimatedPressable>
  );
}

interface ProviderEditorSheetProps {
  editor: ProvidersEditorState;
  mutating: boolean;
  apiKeyAutoFocus?: boolean;
  onClose(): void;
  onTestConnection(input: {
    client: ProviderClient;
    baseUrl: string;
    apiKey: string;
  }): Promise<ProviderConnectionTestResult>;
  onSaveCustom(input: {
    client: ProviderClient;
    baseUrl: string;
    apiKey: string;
  }): Promise<ProviderSaveOutcome> | ProviderSaveOutcome;
  onSaveCredential(
    connection: ProviderConnection,
    apiKey: string,
  ): Promise<ProviderSaveOutcome> | ProviderSaveOutcome;
}

type TestState =
  | { kind: "idle" }
  | { kind: "testing" }
  | { kind: "success"; modelCount: number; latencyMs: number }
  | { kind: "error"; message: string };

function ProviderEditorSheet({
  editor,
  mutating,
  apiKeyAutoFocus,
  onClose,
  onTestConnection,
  onSaveCustom,
  onSaveCredential,
}: ProviderEditorSheetProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const insets = useSafeAreaInsets();
  const [apiKey, setApiKey] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [testState, setTestState] = useState<TestState>({ kind: "idle" });
  const sessionKey = providerEditorSessionKey(editor);
  const prevSessionKeyRef = useRef(sessionKey);

  useEffect(() => {
    const previous = prevSessionKeyRef.current;
    prevSessionKeyRef.current = sessionKey;
    if (providerEditorShouldResetFields(previous, sessionKey)) {
      setApiKey("");
      setBaseUrl("");
      setTestState({ kind: "idle" });
    }
  }, [sessionKey]);

  const resetFields = () => {
    setApiKey("");
    setBaseUrl("");
    setTestState({ kind: "idle" });
  };
  const handleClose = () => {
    resetFields();
    onClose();
  };
  const connection = editor?.kind === "credential" ? editor.connection : null;
  const custom = editor?.kind === "custom" ? editor : null;
  const client = custom?.client ?? connection?.clients[0];
  const normalizedClient =
    client === "codex" || client === "claude" ? client : null;
  const endpoint = custom ? baseUrl : connection?.base_url ?? "";
  const testing = testState.kind === "testing";
  const canSave = providerEditorCanSave({
    mutating: mutating || testing,
    apiKey,
    credentialMode: connection != null,
    customMode: custom != null,
    baseUrl,
  });
  const canTest =
    !mutating &&
    !testing &&
    normalizedClient != null &&
    endpoint.trim().length > 0 &&
    apiKey.trim().length > 0;
  const title = connection
    ? connection.credential_ready
      ? "Replace API key"
      : editor?.kind === "credential" && editor.retry
        ? "Retry API key"
        : "Add API key"
    : custom
      ? `Connect ${providerClientLabel(custom.client)}`
      : "Connect client";

  const updateBaseUrl = (value: string) => {
    setBaseUrl(value);
    setTestState({ kind: "idle" });
  };
  const updateApiKey = (value: string) => {
    setApiKey(value);
    setTestState({ kind: "idle" });
  };

  const handleTest = async () => {
    if (!canTest || !normalizedClient) return;
    setTestState({ kind: "testing" });
    try {
      const result = await onTestConnection({
        client: normalizedClient,
        baseUrl: endpoint.trim(),
        apiKey,
      });
      setTestState({
        kind: "success",
        modelCount: result.modelCount,
        latencyMs: result.latencyMs,
      });
    } catch (error) {
      setTestState({
        kind: "error",
        message:
          error instanceof Error ? error.message : "Connection test failed.",
      });
    }
  };

  const handleSave = async () => {
    if (!canSave) return;
    const outcome = connection
      ? await onSaveCredential(connection, apiKey)
      : custom
        ? await onSaveCustom({
            client: custom.client,
            baseUrl: baseUrl.trim(),
            apiKey,
          })
        : null;
    if (outcome?.status === "saved") resetFields();
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

        {custom ? (
          <Text style={styles.editorHint}>
            This endpoint applies only to Zen-launched {providerClientLabel(custom.client)} Sessions. Official login remains available as a direct option.
          </Text>
        ) : null}

        {connection ? (
          <>
            <Text style={styles.editorProviderName}>{connection.name}</Text>
            {connection.base_url ? (
              <Text style={styles.editorEndpoint}>{connection.base_url}</Text>
            ) : null}
            {editor?.kind === "credential" && editor.retry ? (
              <Text style={styles.editorRetryHint}>
                The API key was not saved. Check it and try again.
              </Text>
            ) : null}
          </>
        ) : null}

        {custom ? (
          <>
            <Text style={styles.fieldLabel}>Base URL</Text>
            <MobileSingleLineInput
              value={baseUrl}
              onChangeText={updateBaseUrl}
              editable={!mutating && !testing}
              placeholder="https://api.example.com/v1"
              placeholderTextColor={colors.textSecondary}
              accessibilityLabel="Base URL"
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
          onChangeText={updateApiKey}
          editable={!mutating && !testing}
          autoFocus={apiKeyAutoFocus && !mutating}
          placeholder="Paste API key"
          placeholderTextColor={colors.textSecondary}
          accessibilityLabel="API Key"
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          autoComplete="off"
          textContentType="none"
          containerStyle={styles.field}
        />

        {endpoint && normalizedClient ? (
          <AnimatedPressable
            style={[styles.testButton, !canTest && styles.buttonDisabled]}
            preset="press"
            scale={0.99}
            disabled={!canTest}
            accessibilityRole="button"
            accessibilityState={{ disabled: !canTest, busy: testing }}
            onPress={() => void handleTest()}
          >
            {testing ? (
              <ActivityIndicator size="small" color={colors.textPrimary} />
            ) : (
              <Ionicons name="pulse-outline" size={18} color={colors.textPrimary} />
            )}
            <Text style={styles.testButtonText}>
              {testing ? "Testing…" : "Test connection"}
            </Text>
          </AnimatedPressable>
        ) : null}

        {testState.kind === "success" ? (
          <View style={styles.testResult}>
            <Ionicons name="checkmark-circle" size={17} color={colors.success} />
            <Text style={[styles.testResultText, { color: colors.success }]}>
              Connected · {testState.latencyMs} ms
              {testState.modelCount > 0
                ? ` · ${testState.modelCount} models found`
                : ""}
            </Text>
          </View>
        ) : null}
        {testState.kind === "error" ? (
          <View style={styles.testResult}>
            <Ionicons name="alert-circle" size={17} color={colors.dangerText} />
            <Text style={[styles.testResultText, { color: colors.dangerText }]}>
              {testState.message}
            </Text>
          </View>
        ) : null}

        <AnimatedPressable
          style={[styles.saveButton, !canSave && styles.buttonDisabled]}
          preset="press"
          scale={0.98}
          disabled={!canSave}
          accessibilityRole="button"
          accessibilityState={{ disabled: !canSave, busy: mutating }}
          onPress={() => void handleSave()}
        >
          <Text style={styles.saveButtonText}>
            {mutating
              ? "Saving…"
              : connection
                ? "Save API key"
                : "Save connection"}
          </Text>
        </AnimatedPressable>
      </ScrollView>
    </RisingSheet>
  );
}

function ModelSyncSheet({
  picker,
  catalog,
  mutating,
  disabled,
  onClose,
  onSelectModel,
}: {
  picker: ModelSyncPickerState;
  catalog: ProvidersSnapshot;
  mutating: boolean;
  disabled: boolean;
  onClose(): void;
  onSelectModel(
    client: ProviderClient,
    connection: ProviderConnection,
    modelId: string,
  ): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const insets = useSafeAreaInsets();
  const choices = modelSupportChoices(
    catalog,
    picker.connection,
    picker.models,
  );
  const enabledCount = choices.filter((choice) => choice.current).length;
  const saving = mutating;

  return (
    <RisingSheet
      visible
      onClose={onClose}
      align="bottom"
      cardStyle={[
        styles.pickerCard,
        { paddingBottom: Math.max(insets.bottom, 16) },
      ]}
    >
      <View style={styles.pickerHeader}>
        <View style={styles.pickerCopy}>
          <Text style={styles.pickerTitle} accessibilityRole="header">
            Models
          </Text>
          <Text style={styles.pickerSubtitle} numberOfLines={1}>
            {picker.connection.name}
          </Text>
        </View>
        <Pressable
          accessibilityLabel="Close model picker"
          accessibilityRole="button"
          hitSlop={8}
          style={styles.editorClose}
          onPress={onClose}
        >
          <Ionicons name="close" size={22} color={colors.textSecondary} />
        </Pressable>
      </View>
      <Text style={styles.pickerHint}>
        {enabledCount === choices.length
          ? `${enabledCount} models exposed`
          : `${enabledCount} of ${choices.length} models exposed`}
        {" · "}tap to toggle support.
      </Text>
      <View style={styles.chipWrap}>
        {choices.map((choice) => {
          const selected = choice.current;
          const chipDisabled = saving || disabled;
          return (
            <Pressable
              key={choice.model.id}
              style={[
                styles.modelChip,
                selected && styles.modelChipSelected,
                chipDisabled && styles.modelChipDisabled,
              ]}
              disabled={chipDisabled}
              accessibilityRole="checkbox"
              accessibilityState={{
                checked: selected,
                disabled: chipDisabled,
                busy: saving,
              }}
              accessibilityLabel={`${choice.model.id}, ${selected ? "exposed" : "hidden"}`}
              onPress={() =>
                onSelectModel(picker.client, picker.connection, choice.model.id)
              }
            >
              <Ionicons
                name={selected ? "checkmark-circle" : "ellipse-outline"}
                size={15}
                color={selected ? colors.accentStrong : colors.textTertiary}
              />
              <Text
                style={[
                  styles.modelChipText,
                  selected && { color: colors.accentStrong },
                ]}
                numberOfLines={1}
              >
                {choice.model.id}
              </Text>
            </Pressable>
          );
        })}
        {saving ? (
          <View style={styles.pickerSavingRow}>
            <ActivityIndicator size="small" color={colors.accent} />
            <Text style={styles.pickerSavingText}>Saving…</Text>
          </View>
        ) : null}
        {!saving && choices.length === 0 ? (
          <Text style={styles.pickerEmpty}>
            No models were reported by this endpoint.
          </Text>
        ) : null}
      </View>
    </RisingSheet>
  );
}

function createStyles(colors: ReturnType<typeof useAppColors>) {
  return StyleSheet.create({
    safe: { flex: 1, backgroundColor: colors.bgPrimary },
    content: {
      paddingHorizontal: 16,
      paddingTop: 16,
      paddingBottom: 36,
      gap: 18,
    },
    center: { paddingVertical: 48, alignItems: "center" },
    stateBlock: {
      padding: 14,
      borderRadius: Radii.xs,
      backgroundColor: colors.bgSurface,
      gap: 6,
    },
    stateTitle: { ...UiTextMetrics, ...TypeScale.title, color: colors.textPrimary },
    stateBody: { ...UiTextMetrics, ...TypeScale.body, color: colors.textSecondary },
    link: { ...UiTextMetrics, ...TypeScale.body, color: colors.accent, fontWeight: "600" },
    notice: {
      padding: 12,
      borderRadius: Radii.xs,
      borderLeftWidth: 3,
      backgroundColor: colors.bgSurface,
      gap: 6,
    },
    noticeText: { ...UiTextMetrics, ...TypeScale.caption, color: colors.textPrimary },
    intro: { gap: 5, paddingHorizontal: 2 },
    introTitle: { ...UiTextMetrics, ...TypeScale.title, color: colors.textPrimary },
    introBody: { ...UiTextMetrics, ...TypeScale.caption, color: colors.textSecondary },
    clientSection: { gap: 10 },
    clientHeader: {
      minHeight: 46,
      flexDirection: "row",
      alignItems: "center",
      gap: 11,
      paddingHorizontal: 2,
    },
    clientIcon: {
      width: 36,
      height: 36,
      borderRadius: 18,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfaceSubtle,
    },
    clientTitle: { ...UiTextMetrics, ...TypeScale.compact, color: colors.textPrimary, fontWeight: "700" },
    clientSummary: { ...UiTextMetrics, ...TypeScale.caption, color: colors.textTertiary, marginTop: 1 },
    group: {
      overflow: "hidden",
      borderRadius: Radii.xs,
      backgroundColor: colors.bgSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    groupRowBorder: { borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.borderSubtle },
    choiceRow: {
      minHeight: 66,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingHorizontal: 14,
      paddingVertical: 10,
    },
    connectionRow: { minHeight: 66, flexDirection: "row", alignItems: "stretch" },
    connectionSelect: {
      flex: 1,
      minWidth: 0,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingLeft: 14,
      paddingVertical: 10,
    },
    expandButton: { width: 52, alignItems: "center", justifyContent: "center" },
    radioOuter: {
      width: 20,
      height: 20,
      borderRadius: 10,
      borderWidth: 1.5,
      borderColor: colors.accent,
      alignItems: "center",
      justifyContent: "center",
    },
    radioInner: { width: 10, height: 10, borderRadius: 5, backgroundColor: colors.accent },
    rowCopy: { flex: 1, minWidth: 0 },
    rowTitle: { ...UiTextMetrics, ...TypeScale.body, color: colors.textPrimary },
    rowSubtitle: { ...UiTextMetrics, ...TypeScale.caption, color: colors.textTertiary, marginTop: 2 },
    rowModel: { ...UiTextMetrics, ...TypeScale.micro, color: colors.accent, marginTop: 3 },
    rowModelHint: { ...UiTextMetrics, ...TypeScale.micro, color: colors.warning, marginTop: 3 },
    keyRequired: { ...UiTextMetrics, ...TypeScale.micro, color: colors.warning, paddingHorizontal: 4 },
    connectionActions: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 8,
      paddingHorizontal: 14,
      paddingTop: 10,
      paddingBottom: 12,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    actionButton: {
      minHeight: 40,
      minWidth: 110,
      flexGrow: 1,
      paddingHorizontal: 12,
      borderRadius: Radii.xs,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfacePressed,
    },
    actionButtonPrimary: { backgroundColor: colors.accentSoft },
    actionButtonDanger: { backgroundColor: colors.dangerSoft },
    actionButtonText: { ...UiTextMetrics, ...TypeScale.label, color: colors.textPrimary },
    actionButtonPrimaryText: { color: colors.accentStrong },
    actionButtonDangerText: { color: colors.dangerText },
    addEndpoint: {
      minHeight: 46,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 7,
      borderRadius: Radii.xs,
      backgroundColor: colors.accentSoft,
    },
    addEndpointText: { ...UiTextMetrics, ...TypeScale.label, color: colors.accentStrong, fontWeight: "600" },
    editorCard: {
      backgroundColor: colors.bgSurface,
      borderTopLeftRadius: Radii.sm,
      borderTopRightRadius: Radii.sm,
      overflow: "hidden",
      maxHeight: 600,
    },
    editorContent: { paddingHorizontal: 16, paddingTop: 8, gap: 8 },
    editorHeader: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", marginBottom: 2 },
    editorTitle: { ...UiTextMetrics, ...TypeScale.title, color: colors.textPrimary },
    editorClose: {
      width: 36,
      height: 36,
      borderRadius: 18,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfaceSubtle,
    },
    pickerCard: {
      backgroundColor: colors.bgSurface,
      borderTopLeftRadius: Radii.sm,
      borderTopRightRadius: Radii.sm,
      overflow: "hidden",
      maxHeight: 560,
    },
    pickerHeader: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 10,
      paddingHorizontal: 16,
      paddingTop: 14,
      paddingBottom: 4,
    },
    pickerCopy: { flex: 1, minWidth: 0 },
    pickerTitle: { ...UiTextMetrics, ...TypeScale.title, color: colors.textPrimary },
    pickerSubtitle: { ...UiTextMetrics, ...TypeScale.caption, color: colors.textTertiary, marginTop: 2 },
    pickerHint: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textSecondary,
      paddingHorizontal: 16,
      paddingBottom: 10,
    },
    pickerList: { paddingHorizontal: 16, paddingBottom: 8 },
    chipWrap: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 8,
      paddingHorizontal: 16,
      paddingBottom: 8,
    },
    modelChip: {
      minHeight: 34,
      maxWidth: "100%",
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      borderRadius: 17,
      borderWidth: 1,
      borderColor: colors.border,
      backgroundColor: colors.surfacePressed,
      paddingHorizontal: 11,
      paddingVertical: 6,
    },
    modelChipSelected: {
      borderColor: colors.accent,
      backgroundColor: colors.accentSoft,
    },
    modelChipDisabled: { opacity: 0.45 },
    modelChipText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textSecondary,
      flexShrink: 1,
    },
    pickerSavingRow: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 8,
      paddingVertical: 14,
    },
    pickerSavingText: { ...UiTextMetrics, ...TypeScale.body, color: colors.textSecondary },
    pickerEmpty: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.textTertiary,
      textAlign: "center",
      paddingVertical: 20,
    },
    editorHint: { ...UiTextMetrics, ...TypeScale.caption, color: colors.textSecondary, marginBottom: 4 },
    editorProviderName: { ...UiTextMetrics, ...TypeScale.body, color: colors.textPrimary, fontWeight: "600" },
    editorEndpoint: { ...UiTextMetrics, ...TypeScale.caption, color: colors.textTertiary },
    editorRetryHint: { ...UiTextMetrics, ...TypeScale.caption, color: colors.warning, marginTop: 4 },
    fieldLabel: { ...UiTextMetrics, ...TypeScale.label, color: colors.textSecondary, marginTop: 8, marginBottom: 2, marginHorizontal: 2 },
    field: {
      borderRadius: Radii.xs,
      backgroundColor: colors.inputBackground,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
    },
    testButton: {
      minHeight: 46,
      marginTop: 8,
      borderRadius: Radii.xs,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 8,
      backgroundColor: colors.surfacePressed,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    testButtonText: { ...UiTextMetrics, ...TypeScale.body, color: colors.textPrimary, fontWeight: "600" },
    testResult: { flexDirection: "row", alignItems: "flex-start", gap: 7, paddingHorizontal: 2, paddingTop: 2 },
    testResultText: { ...UiTextMetrics, ...TypeScale.caption, flex: 1 },
    saveButton: {
      minHeight: 50,
      marginTop: 8,
      borderRadius: Radii.xs,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.accent,
    },
    buttonDisabled: { opacity: 0.45 },
    saveButtonText: { ...UiTextMetrics, ...TypeScale.body, color: colors.textOnAccent, fontWeight: "700" },
  });
}
