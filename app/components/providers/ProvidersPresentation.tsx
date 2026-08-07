import React, { useMemo } from "react";
import {
  ActivityIndicator,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import { Radii, TypeScale, useAppColors } from "../../constants/tokens";
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

export type ProvidersScreenMode = "list" | "add" | "credential";

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
  mode: ProvidersScreenMode;
  mutating: boolean;
  onRefresh(): void;
  onOpenSettings(): void;
  onAdd(): void;
  onAddApiKey(connection: ProviderConnection): void;
  onClearCredential(connection: ProviderConnection): void;
  onDelete(connection: ProviderConnection): void;
  onSetDefault(client: string, connection: ProviderConnection): void;
  onDiscover(connection: ProviderConnection): void;
  addSlot?: React.ReactNode;
  credentialSlot?: React.ReactNode;
}

function clientLabel(client: string): string {
  const c = client.trim().toLowerCase();
  if (c === "codex") return "Codex";
  if (c === "claude") return "Claude";
  return client;
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
  mode,
  mutating,
  onRefresh,
  onOpenSettings,
  onAdd,
  onAddApiKey,
  onClearCredential,
  onDelete,
  onSetDefault,
  onDiscover,
  addSlot,
  credentialSlot,
}: ProvidersPresentationProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const connections = catalog?.connections ?? [];
  const defaultRows = futureDefaultRows(catalog);

  if (mode === "add" && addSlot) {
    return <>{addSlot}</>;
  }
  if (mode === "credential" && credentialSlot) {
    return <>{credentialSlot}</>;
  }

  return (
    <SafeAreaView style={styles.safe} edges={["bottom"]}>
      <ScrollView
        contentContainerStyle={styles.content}
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
          <View style={[styles.banner, styles.warnBanner]}>
            <Text style={styles.bannerText}>{durabilityWarning}</Text>
          </View>
        ) : null}

        {requiresRefreshBeforeMutation ? (
          <View style={[styles.banner, styles.lockBanner]}>
            <Text style={styles.bannerText}>
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

        {catalog && connections.length > 0 ? (
          <View
            style={styles.defaultsSection}
            accessibilityLabel="Future defaults"
          >
            <Text style={styles.defaultsTitle}>Defaults</Text>
            <Text style={styles.stateBody}>
              Choose which ready Provider new Codex and Claude Sessions use.
            </Text>
            {defaultRows.map((row) => (
              <View key={row.client} style={styles.defaultClientRow}>
                <Text style={styles.defaultLabel}>{row.label}</Text>
                <Text style={styles.meta}>
                  {row.currentConnectionName
                    ? `Current · ${row.currentConnectionName}`
                    : "No future default"}
                </Text>
                {row.options.length === 0 ? (
                  <Text style={styles.meta}>
                    Add a ready {row.label} Provider to set a default.
                  </Text>
                ) : (
                  <View style={styles.row}>
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
                          onPress={() => onSetDefault(row.client, connection)}
                          disabled={
                            mutating ||
                            requiresRefreshBeforeMutation ||
                            option.selected
                          }
                          accessibilityRole="button"
                          accessibilityState={{
                            selected: option.selected,
                            disabled: option.selected,
                          }}
                          accessibilityLabel={`${row.label} default ${option.connectionName}`}
                        >
                          <Text style={styles.actionText}>
                            {option.connectionName}
                          </Text>
                        </AnimatedPressable>
                      );
                    })}
                  </View>
                )}
              </View>
            ))}
          </View>
        ) : null}

        {catalog && connections.length === 0 && !loading ? (
          <View style={styles.stateBlock}>
            <Text style={styles.stateTitle}>No Providers yet</Text>
            <Text style={styles.stateBody}>
              Add a curated Provider such as DeepSeek, or a Custom Gateway.
            </Text>
          </View>
        ) : null}

        {connections.map((connection) => {
          const defaults = defaultClientsForConnection(catalog!, connection.id);
          const clients = connection.clients ?? [];
          const defaultLabel =
            defaults.length > 0
              ? `Default · ${defaults.map(clientLabel).join(", ")}`
              : "Not a future default";
          return (
            <ProviderConnectionCard
              key={connection.id}
              connection={connection}
              clients={clients}
              defaultLabel={defaultLabel}
              mutating={mutating}
              requiresRefreshBeforeMutation={requiresRefreshBeforeMutation}
              onAddApiKey={onAddApiKey}
              onClearCredential={onClearCredential}
              onDelete={onDelete}
              onDiscover={onDiscover}
            />
          );
        })}
      </ScrollView>

      {currentServerAvailable && !offline && !unavailable ? (
        <View style={styles.footer}>
          <AnimatedPressable
            style={styles.primaryBtn}
            onPress={onAdd}
            disabled={mutating || requiresRefreshBeforeMutation}
            accessibilityRole="button"
            accessibilityLabel="Add Provider"
          >
            <Ionicons name="add" size={18} color="#fff" />
            <Text style={styles.primaryText}>Add Provider</Text>
          </AnimatedPressable>
        </View>
      ) : null}
    </SafeAreaView>
  );
}

function ProviderConnectionCard({
  connection,
  clients,
  defaultLabel,
  mutating,
  requiresRefreshBeforeMutation,
  onAddApiKey,
  onClearCredential,
  onDelete,
  onDiscover,
}: {
  connection: ProviderConnection;
  clients: string[];
  defaultLabel: string;
  mutating: boolean;
  requiresRefreshBeforeMutation?: boolean;
  onAddApiKey(connection: ProviderConnection): void;
  onClearCredential(connection: ProviderConnection): void;
  onDelete(connection: ProviderConnection): void;
  onDiscover(connection: ProviderConnection): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const [manageOpen, setManageOpen] = React.useState(false);

  return (
    <View
      style={styles.card}
      accessibilityLabel={`Provider ${connection.name}`}
    >
      <View style={styles.cardHeader}>
        <Text style={styles.cardTitle}>{connection.name}</Text>
        <View
          style={[
            styles.readyPill,
            {
              backgroundColor: connection.credential_ready
                ? colors.successSoft
                : colors.warningSoft,
            },
          ]}
        >
          <Text
            style={[
              styles.readyText,
              {
                color: connection.credential_ready
                  ? colors.success
                  : colors.warning,
              },
            ]}
          >
            {connection.credential_ready ? "Ready" : "API key needed"}
          </Text>
        </View>
      </View>
      {clients.length > 0 ? (
        <Text style={styles.meta}>
          Clients: {clients.map(clientLabel).join(", ")}
        </Text>
      ) : null}
      <Text style={styles.meta}>{defaultLabel}</Text>

      {!connection.credential_ready ? (
        <AnimatedPressable
          style={styles.primaryInline}
          onPress={() => onAddApiKey(connection)}
          disabled={mutating || requiresRefreshBeforeMutation}
          accessibilityRole="button"
          accessibilityLabel="Add API key"
        >
          <Text style={styles.primaryInlineText}>Add API key</Text>
        </AnimatedPressable>
      ) : null}

      <AnimatedPressable
        style={styles.manageToggle}
        onPress={() => setManageOpen((open) => !open)}
        accessibilityRole="button"
        accessibilityLabel={manageOpen ? "Hide manage options" : "Manage"}
      >
        <Text style={styles.actionText}>
          {manageOpen ? "Hide manage" : "Manage"}
        </Text>
        <Ionicons
          name={manageOpen ? "chevron-up" : "chevron-down"}
          size={16}
          color={colors.textSecondary}
        />
      </AnimatedPressable>

      {manageOpen ? (
        <View style={styles.actions}>
          {connection.credential_ready ? (
            <>
              <AnimatedPressable
                style={styles.actionBtn}
                onPress={() => onAddApiKey(connection)}
                disabled={mutating || requiresRefreshBeforeMutation}
                accessibilityRole="button"
                accessibilityLabel="Replace API key"
              >
                <Text style={styles.actionText}>Replace key</Text>
              </AnimatedPressable>
              <AnimatedPressable
                style={styles.actionBtn}
                onPress={() => onClearCredential(connection)}
                disabled={mutating || requiresRefreshBeforeMutation}
                accessibilityRole="button"
                accessibilityLabel="Clear API key"
              >
                <Text style={styles.actionText}>Clear key</Text>
              </AnimatedPressable>
              <AnimatedPressable
                style={styles.actionBtn}
                onPress={() => onDiscover(connection)}
                disabled={mutating || requiresRefreshBeforeMutation}
                accessibilityRole="button"
                accessibilityLabel="Refresh models"
              >
                <Text style={styles.actionText}>Refresh models</Text>
              </AnimatedPressable>
            </>
          ) : null}
          <AnimatedPressable
            style={[styles.actionBtn, styles.dangerBtn]}
            onPress={() => onDelete(connection)}
            disabled={mutating || requiresRefreshBeforeMutation}
            accessibilityRole="button"
            accessibilityLabel="Delete provider"
          >
            <Text style={[styles.actionText, styles.dangerText]}>Delete</Text>
          </AnimatedPressable>
        </View>
      ) : null}
    </View>
  );
}

export interface ProviderAddFormProps {
  presets: ProviderPreset[];
  mutating: boolean;
  onCancel(): void;
  onSaveCurated(input: {
    preset: ProviderPreset;
    apiKey: string;
  }): Promise<void> | void;
  onSaveCustom(input: {
    name: string;
    client: string;
    baseUrl: string;
    apiKey: string;
    modelId?: string;
  }): Promise<void> | void;
}

export function ProviderAddForm({
  presets,
  mutating,
  onCancel,
  onSaveCurated,
  onSaveCustom,
}: ProviderAddFormProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const curated = presets.filter((p) => p.advanced !== true);
  const custom = presets.find((p) => p.advanced === true || p.id === "custom");
  const [step, setStep] = React.useState<"pick" | "curated" | "custom">("pick");
  const [preset, setPreset] = React.useState<ProviderPreset | null>(null);
  const [apiKey, setApiKey] = React.useState("");
  const [name, setName] = React.useState("");
  const [client, setClient] = React.useState("codex");
  const [baseUrl, setBaseUrl] = React.useState("");
  const [modelId, setModelId] = React.useState("");

  const clearKey = () => setApiKey("");

  return (
    <SafeAreaView style={styles.safe} edges={["bottom"]}>
      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        <View style={styles.formHeader}>
          <Text style={styles.stateTitle} accessibilityRole="header">
            {step === "pick"
              ? "Add Provider"
              : step === "curated"
                ? preset?.label ?? "Provider"
                : "Custom Gateway"}
          </Text>
          <Pressable onPress={onCancel} accessibilityRole="button" accessibilityLabel="Cancel">
            <Text style={styles.link}>Cancel</Text>
          </Pressable>
        </View>

        {step === "pick" ? (
          <View style={styles.stack}>
            {curated.map((item) => (
              <AnimatedPressable
                key={item.id}
                style={styles.card}
                onPress={() => {
                  setPreset(item);
                  setStep("curated");
                }}
                accessibilityRole="button"
                accessibilityLabel={`Add ${item.label}`}
              >
                <Text style={styles.cardTitle}>{item.label}</Text>
                <Text style={styles.meta}>
                  {(item.clients ?? []).map(clientLabel).join(", ") || "Codex, Claude"}
                </Text>
              </AnimatedPressable>
            ))}
            {custom ? (
              <AnimatedPressable
                style={styles.card}
                onPress={() => setStep("custom")}
                accessibilityRole="button"
                accessibilityLabel="Add Custom Gateway"
              >
                <Text style={styles.cardTitle}>Custom Gateway</Text>
                <Text style={styles.meta}>Advanced · name, client, base URL, key</Text>
              </AnimatedPressable>
            ) : null}
          </View>
        ) : null}

        {step === "curated" && preset ? (
          <View style={styles.stack}>
            <Text style={styles.label}>API Key</Text>
            <TextInput
              style={styles.input}
              value={apiKey}
              onChangeText={setApiKey}
              secureTextEntry
              autoCapitalize="none"
              autoCorrect={false}
              placeholder="Paste API key"
              placeholderTextColor={colors.textTertiary}
              editable={!mutating}
              accessibilityLabel="API Key"
            />
            <AnimatedPressable
              style={styles.primaryBtn}
              disabled={mutating || !apiKey.trim()}
              onPress={async () => {
                const key = apiKey;
                clearKey();
                await onSaveCurated({ preset, apiKey: key });
              }}
              accessibilityRole="button"
              accessibilityLabel="Save"
            >
              <Text style={styles.primaryText}>{mutating ? "Saving…" : "Save"}</Text>
            </AnimatedPressable>
          </View>
        ) : null}

        {step === "custom" ? (
          <View style={styles.stack}>
            <Text style={styles.label}>Display name</Text>
            <TextInput
              style={styles.input}
              value={name}
              onChangeText={setName}
              placeholder="My gateway"
              placeholderTextColor={colors.textTertiary}
              editable={!mutating}
            />
            <Text style={styles.label}>Client</Text>
            <View style={styles.row}>
              {(["codex", "claude"] as const).map((c) => (
                <AnimatedPressable
                  key={c}
                  style={[
                    styles.chip,
                    client === c && { backgroundColor: colors.accentSoft },
                  ]}
                  onPress={() => setClient(c)}
                  disabled={mutating}
                >
                  <Text style={styles.actionText}>{clientLabel(c)}</Text>
                </AnimatedPressable>
              ))}
            </View>
            <Text style={styles.label}>Base URL</Text>
            <TextInput
              style={styles.input}
              value={baseUrl}
              onChangeText={setBaseUrl}
              autoCapitalize="none"
              autoCorrect={false}
              placeholder="https://…"
              placeholderTextColor={colors.textTertiary}
              editable={!mutating}
            />
            <Text style={styles.label}>API Key</Text>
            <TextInput
              style={styles.input}
              value={apiKey}
              onChangeText={setApiKey}
              secureTextEntry
              autoCapitalize="none"
              autoCorrect={false}
              placeholder="Paste API key"
              placeholderTextColor={colors.textTertiary}
              editable={!mutating}
              accessibilityLabel="API Key"
            />
            <Text style={styles.label}>Manual model ID (optional)</Text>
            <TextInput
              style={styles.input}
              value={modelId}
              onChangeText={setModelId}
              autoCapitalize="none"
              autoCorrect={false}
              placeholder="model-id"
              placeholderTextColor={colors.textTertiary}
              editable={!mutating}
            />
            <AnimatedPressable
              style={styles.primaryBtn}
              disabled={
                mutating ||
                !name.trim() ||
                !baseUrl.trim() ||
                !apiKey.trim()
              }
              onPress={async () => {
                const key = apiKey;
                clearKey();
                await onSaveCustom({
                  name: name.trim(),
                  client,
                  baseUrl: baseUrl.trim(),
                  apiKey: key,
                  modelId: modelId.trim() || undefined,
                });
              }}
              accessibilityRole="button"
              accessibilityLabel="Save"
            >
              <Text style={styles.primaryText}>{mutating ? "Saving…" : "Save"}</Text>
            </AnimatedPressable>
          </View>
        ) : null}
      </ScrollView>
    </SafeAreaView>
  );
}

export interface ProviderCredentialFormProps {
  connection: ProviderConnection;
  mutating: boolean;
  onCancel(): void;
  onSave(apiKey: string): Promise<void> | void;
}

export function ProviderCredentialForm({
  connection,
  mutating,
  onCancel,
  onSave,
}: ProviderCredentialFormProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const [apiKey, setApiKey] = React.useState("");

  return (
    <SafeAreaView style={styles.safe} edges={["bottom"]}>
      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        <View style={styles.formHeader}>
          <Text style={styles.stateTitle} accessibilityRole="header">
            {connection.credential_ready ? "Replace API key" : "Add API key"}
          </Text>
          <Pressable onPress={onCancel} accessibilityRole="button">
            <Text style={styles.link}>Cancel</Text>
          </Pressable>
        </View>
        <Text style={styles.meta}>{connection.name}</Text>
        <Text style={styles.label}>API Key</Text>
        <TextInput
          style={styles.input}
          value={apiKey}
          onChangeText={setApiKey}
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          placeholder="Paste API key"
          placeholderTextColor={colors.textTertiary}
          editable={!mutating}
          accessibilityLabel="API Key"
        />
        <AnimatedPressable
          style={styles.primaryBtn}
          disabled={mutating || !apiKey.trim()}
          onPress={async () => {
            const key = apiKey;
            setApiKey("");
            await onSave(key);
          }}
          accessibilityRole="button"
          accessibilityLabel="Save"
        >
          <Text style={styles.primaryText}>{mutating ? "Saving…" : "Save"}</Text>
        </AnimatedPressable>
      </ScrollView>
    </SafeAreaView>
  );
}

function createStyles(colors: ReturnType<typeof useAppColors>) {
  return StyleSheet.create({
    safe: { flex: 1, backgroundColor: colors.bgPrimary },
    content: { padding: 16, paddingBottom: 120, gap: 12 },
    center: { paddingVertical: 48, alignItems: "center" },
    stateBlock: {
      padding: 16,
      borderRadius: Radii.lg,
      backgroundColor: colors.bgSurface,
      gap: 8,
    },
    stateTitle: {
      ...TypeScale.title,
      color: colors.textPrimary,
    },
    stateBody: {
      ...TypeScale.body,
      color: colors.textSecondary,
    },
    link: {
      ...TypeScale.body,
      color: colors.accent,
      fontWeight: "600",
    },
    banner: {
      padding: 12,
      borderRadius: Radii.md,
      gap: 6,
    },
    warnBanner: { backgroundColor: colors.warningSoft },
    lockBanner: { backgroundColor: colors.accentSoft },
    bannerText: { ...TypeScale.caption, color: colors.textPrimary },
    card: {
      padding: 14,
      borderRadius: Radii.lg,
      backgroundColor: colors.bgSurface,
      gap: 8,
    },
    cardHeader: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 8,
    },
    cardTitle: { ...TypeScale.title, color: colors.textPrimary, flex: 1 },
    readyPill: {
      paddingHorizontal: 8,
      paddingVertical: 4,
      borderRadius: Radii.sm,
    },
    readyText: { ...TypeScale.caption, fontWeight: "600" },
    meta: { ...TypeScale.caption, color: colors.textSecondary },
    actions: { flexDirection: "row", flexWrap: "wrap", gap: 8, marginTop: 4 },
    actionBtn: {
      paddingHorizontal: 10,
      paddingVertical: 8,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
    },
    actionText: {
      ...TypeScale.caption,
      color: colors.textPrimary,
      fontWeight: "600",
    },
    dangerBtn: { backgroundColor: colors.dangerSoft },
    dangerText: { color: colors.dangerText },
    footer: {
      position: "absolute",
      left: 16,
      right: 16,
      bottom: 24,
    },
    primaryBtn: {
      backgroundColor: colors.accent,
      borderRadius: Radii.lg,
      paddingVertical: 14,
      paddingHorizontal: 16,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 8,
    },
    primaryText: {
      ...TypeScale.body,
      color: colors.textOnAccent,
      fontWeight: "700",
    },
    formHeader: {
      flexDirection: "row",
      justifyContent: "space-between",
      alignItems: "center",
      marginBottom: 8,
    },
    stack: { gap: 12 },
    label: {
      ...TypeScale.caption,
      color: colors.textSecondary,
      fontWeight: "600",
    },
    input: {
      borderRadius: Radii.md,
      backgroundColor: colors.inputBackground,
      paddingHorizontal: 12,
      paddingVertical: 12,
      color: colors.textPrimary,
      ...TypeScale.body,
    },
    row: { flexDirection: "row", gap: 8, flexWrap: "wrap" },
    chip: {
      paddingHorizontal: 12,
      paddingVertical: 8,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
    },
    primaryInline: {
      alignSelf: "flex-start",
      backgroundColor: colors.accent,
      borderRadius: Radii.sm,
      paddingHorizontal: 12,
      paddingVertical: 8,
    },
    primaryInlineText: {
      ...TypeScale.caption,
      color: colors.textOnAccent,
      fontWeight: "700",
    },
    defaultsSection: {
      padding: 14,
      borderRadius: Radii.lg,
      backgroundColor: colors.bgSurface,
      gap: 10,
    },
    defaultsTitle: {
      ...TypeScale.title,
      color: colors.textPrimary,
    },
    defaultClientRow: { gap: 6 },
    defaultLabel: {
      ...TypeScale.caption,
      color: colors.textSecondary,
      fontWeight: "600",
    },
    manageToggle: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      paddingVertical: 4,
    },
  });
}
