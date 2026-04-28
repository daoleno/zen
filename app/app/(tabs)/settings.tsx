import React, { useEffect, useMemo, useState } from "react";
import {
  Alert,
  KeyboardAvoidingView,
  Modal,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native";
import { useFocusEffect, useLocalSearchParams } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import * as Clipboard from "expo-clipboard";
import * as DocumentPicker from "expo-document-picker";
import {
  BarcodeScanningResult,
  BarcodeType,
  CameraView,
  scanFromURLAsync,
  useCameraPermissions,
} from "expo-camera";
import { Colors, Typography, statusColor } from "../../constants/tokens";
import {
  DefaultTerminalThemeName,
  TerminalThemeLabels,
  TerminalThemeName,
  TerminalThemes,
} from "../../constants/terminalThemes";
import { importConnection } from "../../services/importConnection";
import { wsClient } from "../../services/websocket";
import { ConnectionState, useAgents } from "../../store/agents";
import * as Storage from "../../services/storage";
import { connectionIssueAccent } from "../../services/connectionIssue";

const QR_BARCODE_TYPES: BarcodeType[] = ["qr"];

export default function SettingsScreen() {
  const { state, dispatch } = useAgents();
  const params = useLocalSearchParams<{
    addServer?: string;
    refresh?: string;
  }>();
  const [servers, setServers] = useState<Storage.StoredServer[]>([]);
  const [terminalTheme, setTerminalTheme] = useState<TerminalThemeName>(
    DefaultTerminalThemeName,
  );
  const [loaded, setLoaded] = useState(false);
  const [editorVisible, setEditorVisible] = useState(false);
  const [scannerVisible, setScannerVisible] = useState(false);
  const [scannerLocked, setScannerLocked] = useState(false);
  const [editingServerId, setEditingServerId] = useState<string | null>(null);
  const [draftName, setDraftName] = useState("");
  const [draftEndpoint, setDraftEndpoint] = useState("");
  const [draftImportValue, setDraftImportValue] = useState("");
  const [expandedServer, setExpandedServer] = useState<string | null>(null);
  const [handledAutoOpenToken, setHandledAutoOpenToken] = useState<
    string | null
  >(null);
  const [handledRefreshToken, setHandledRefreshToken] = useState<string | null>(
    null,
  );
  const [cameraPermission, requestCameraPermission] = useCameraPermissions();

  const connectedCount = useMemo(
    () =>
      servers.filter(
        (server) => state.serverConnections[server.id] === "connected",
      ).length,
    [servers, state.serverConnections],
  );
  const agentCountByServer = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const agent of state.agents) {
      counts[agent.serverId] = (counts[agent.serverId] || 0) + 1;
    }
    return counts;
  }, [state.agents]);
  const editingServer = useMemo(
    () => servers.find((server) => server.id === editingServerId) || null,
    [editingServerId, servers],
  );
  const serverLatencyById = state.serverLatencyById;

  useFocusEffect(
    React.useCallback(() => {
      let cancelled = false;

      (async () => {
        const [savedServers, theme] = await Promise.all([
          Storage.getServers(),
          Storage.getTerminalTheme(),
        ]);
        if (cancelled) return;

        setServers(savedServers);
        setTerminalTheme(theme);
        setLoaded(true);
      })();

      return () => {
        cancelled = true;
      };
    }, []),
  );

  useEffect(() => {
    if (
      !loaded ||
      !params.addServer ||
      handledAutoOpenToken === params.addServer
    )
      return;
    openCreateServer();
    setHandledAutoOpenToken(params.addServer);
  }, [handledAutoOpenToken, loaded, params.addServer]);

  useEffect(() => {
    if (!loaded || !params.refresh || handledRefreshToken === params.refresh)
      return;
    void refreshServers();
    setHandledRefreshToken(params.refresh);
  }, [handledRefreshToken, loaded, params.refresh]);

  const refreshServers = async () => {
    setServers(await Storage.getServers());
  };

  const connectServer = async (server: Storage.StoredServer) => {
    await Storage.setServerAutoConnect(server.id, true);
    wsClient.connectServer(server);
  };

  const disconnectServer = async (serverId: string) => {
    await Storage.setServerAutoConnect(serverId, false);
    wsClient.disconnectServer(serverId);
  };

  const openCreateServer = () => {
    setEditingServerId(null);
    setDraftName("");
    setDraftEndpoint("");
    setDraftImportValue("");
    setEditorVisible(true);
  };

  const openEditServer = (server: Storage.StoredServer) => {
    setEditingServerId(server.id);
    setDraftName(server.name);
    setDraftEndpoint(server.url);
    setDraftImportValue("");
    setEditorVisible(true);
  };

  const closeEditor = () => {
    setEditorVisible(false);
    setEditingServerId(null);
    setDraftName("");
    setDraftEndpoint("");
    setDraftImportValue("");
  };

  const openScanner = () => {
    setScannerLocked(false);
    setScannerVisible(true);
  };

  const closeScanner = () => {
    setScannerVisible(false);
    setScannerLocked(false);
  };

  const handleSaveServer = async () => {
    if (!editingServer) {
      await handleImportDraft();
      return;
    }

    const normalizedEndpoint = draftEndpoint.trim();
    if (!normalizedEndpoint) {
      Alert.alert(
        "Endpoint required",
        "Enter the WebSocket endpoint exposed by your tunnel or private network.",
      );
      return;
    }

    const previousConnectionState = editingServerId
      ? state.serverConnections[editingServerId]
      : "connected";
    const shouldReconnect =
      previousConnectionState === "connected" ||
      previousConnectionState === "connecting";

    let savedServer: Storage.StoredServer;
    try {
      savedServer = await Storage.saveServer({
        id: editingServer.id,
        name: draftName,
        url: normalizedEndpoint,
        daemonId: editingServer.daemonId,
        daemonPublicKey: editingServer.daemonPublicKey,
      });
    } catch (error: any) {
      Alert.alert(
        "Invalid endpoint",
        error?.message ||
          "Use a full ws://, wss://, http://, or https:// URL that points at zen-daemon.",
      );
      return;
    }

    await refreshServers();
    closeEditor();

    if (shouldReconnect) {
      await Storage.setServerAutoConnect(savedServer.id, true);
      wsClient.connectServer(savedServer);
    }
  };

  const importServer = async (
    rawValue: string,
    options?: { closeScanner?: boolean },
  ) => {
    try {
      const savedServer = await importConnection(rawValue, {
        onImported: async (importedServer) => {
          await refreshServers();
          setExpandedServer(importedServer.id);
        },
      });

      if (!savedServer) {
        Alert.alert(
          "Invalid import",
          "Could not parse the pairing link. Import the zen:// link or QR printed by zen-daemon.",
        );
        return false;
      }

      if (options?.closeScanner) {
        closeScanner();
      }
      closeEditor();
      return true;
    } catch (error: any) {
      Alert.alert(
        "Pairing failed",
        error?.message || "Could not pair with that daemon.",
      );
      return false;
    } finally {
      setScannerLocked(false);
    }
  };

  const handleDeleteServer = (server: Storage.StoredServer) => {
    Alert.alert("Remove server", `Delete ${server.name}?`, [
      { text: "Cancel", style: "cancel" },
      {
        text: "Delete",
        style: "destructive",
        onPress: async () => {
          wsClient.disconnectServer(server.id);
          dispatch({ type: "REMOVE_SERVER", serverId: server.id });
          await Storage.removeServer(server.id);
          await refreshServers();
        },
      },
    ]);
  };

  const handleTerminalTheme = async (value: TerminalThemeName) => {
    setTerminalTheme(value);
    await Storage.setTerminalTheme(value);
  };

  const toggleServerExpand = (serverId: string) => {
    setExpandedServer((prev) => (prev === serverId ? null : serverId));
  };

  const handlePasteImport = async () => {
    const clipboardValue = await Clipboard.getStringAsync();
    if (!clipboardValue.trim()) {
      Alert.alert("Clipboard is empty", "Copy a zen:// pairing link first.");
      return;
    }
    await importServer(clipboardValue);
  };

  const handleImportDraft = async () => {
    const rawValue = draftImportValue.trim();
    if (!rawValue) {
      Alert.alert(
        "Pairing link required",
        "Paste the pairing link printed by zen-daemon, or scan its QR code.",
      );
      return;
    }
    await importServer(rawValue);
  };

  const handleScanResult = async ({ data }: BarcodeScanningResult) => {
    if (scannerLocked) return;
    setScannerLocked(true);
    await importServer(data || "", { closeScanner: true });
  };

  const handlePickScannerImage = async () => {
    if (scannerLocked) return;

    try {
      const result = await DocumentPicker.getDocumentAsync({
        type: ["image/*"],
        copyToCacheDirectory: true,
      });
      if (result.canceled || !result.assets?.length) {
        return;
      }

      const asset = result.assets[0];
      if (!asset.uri) {
        throw new Error("Selected image is not available.");
      }

      setScannerLocked(true);
      const matches = await scanFromURLAsync(asset.uri, QR_BARCODE_TYPES);
      const qrMatch = matches.find((item) => (item.data || "").trim());
      if (!qrMatch?.data) {
        Alert.alert(
          "QR not found",
          "No QR code was detected in that image. Use a tighter crop with the QR filling most of the frame.",
        );
        return;
      }

      await importServer(qrMatch.data, { closeScanner: true });
    } catch (error: any) {
      Alert.alert(
        "Image scan failed",
        error?.message || "Could not read a QR code from that image.",
      );
    } finally {
      setScannerLocked(false);
    }
  };

  if (!loaded) return null;

  return (
    <SafeAreaView style={styles.container} edges={["top"]}>
      <View style={styles.header}>
        <Text style={styles.pageTitle}>Settings</Text>
      </View>
      <ScrollView contentContainerStyle={styles.content}>
        {/* Servers */}
        <View style={styles.sectionHeader}>
          <Text style={[styles.sectionLabel, { marginTop: 0 }]}>Servers</Text>
          {servers.length > 0 && (
            <Text style={styles.sectionCount}>
              {connectedCount}/{servers.length}
            </Text>
          )}
        </View>

        <View style={styles.serverList}>
          {servers.length === 0 ? (
            <View style={styles.emptyCard}>
              <Text style={styles.emptyText}>No paired daemons yet</Text>
            </View>
          ) : (
            servers.map((server) => {
              const connectionState =
                state.serverConnections[server.id] || "offline";
              const latencySample = serverLatencyById[server.id];
              const connectionIssue =
                state.serverConnectionIssues[server.id] || null;
              const expanded = expandedServer === server.id;
              const agentCount = agentCountByServer[server.id] || 0;
              const hydrated = Boolean(state.hydratedServers[server.id]);
              const waitingForAgents =
                connectionState === "connected" &&
                (!hydrated || agentCount === 0);
              const actionLabel =
                connectionState === "connected"
                  ? "Disconnect"
                  : connectionState === "connecting" || connectionIssue
                    ? "Retry"
                    : "Connect";

              return (
                <TouchableOpacity
                  key={server.id}
                  style={styles.serverCard}
                  onPress={() => toggleServerExpand(server.id)}
                  activeOpacity={0.82}
                >
                  <View style={styles.serverRow}>
                    <View
                      style={[
                        styles.statusDot,
                        { backgroundColor: connectionColor(connectionState) },
                      ]}
                    />
                    <View style={styles.serverInfo}>
                      <Text style={styles.serverName}>{server.name}</Text>
                      <Text style={styles.serverUrl} numberOfLines={1}>
                        {server.url}
                      </Text>
                    </View>
                    <View style={styles.serverStatus}>
                      {connectionState === "connected" && latencySample ? (
                        <Text
                          style={[
                            styles.latencyLabel,
                            {
                              color: latencyColor(latencySample.latencyMs),
                            },
                          ]}
                        >
                          {formatLatency(latencySample.latencyMs)}
                        </Text>
                      ) : null}
                      <Text
                        style={[
                          styles.connectionLabel,
                          connectionState === "connected" &&
                            styles.connectionLabelActive,
                        ]}
                      >
                        {connectionLabel(connectionState)}
                      </Text>
                    </View>
                  </View>

                  {expanded && (
                    <>
                      {connectionIssue ? (
                        <ServerNoticeCard
                          icon="alert-circle-outline"
                          accent={connectionIssueAccent(connectionIssue)}
                          title={connectionIssue.title}
                          detail={connectionIssue.detail}
                          hint={connectionIssue.hint}
                        />
                      ) : null}

                      {waitingForAgents ? (
                        <ServerNoticeCard
                          icon="information-circle-outline"
                          accent={Colors.accent}
                          title={
                            hydrated
                              ? "Connected, no active agents yet"
                              : "Connected, waiting for agent data"
                          }
                          detail={
                            hydrated
                              ? "zen is connected to this daemon, but it has not reported any live agents yet."
                              : "zen is connected to this daemon and waiting for the first agent list to arrive."
                          }
                          hint="Start Claude or Codex on that machine, or verify the watcher/tmux bridge is forwarding terminals."
                        />
                      ) : null}

                      <View style={styles.serverActions}>
                        <TouchableOpacity
                          style={styles.actionBtn}
                          onPress={() => void (
                            connectionState === "connected"
                              ? disconnectServer(server.id)
                              : connectServer(server)
                          )}
                          activeOpacity={0.82}
                        >
                          <Text style={styles.actionBtnText}>
                            {actionLabel}
                          </Text>
                        </TouchableOpacity>
                        <TouchableOpacity
                          style={styles.actionBtn}
                          onPress={() => openEditServer(server)}
                          activeOpacity={0.82}
                        >
                          <Text style={styles.actionBtnText}>Edit</Text>
                        </TouchableOpacity>
                        <TouchableOpacity
                          style={[styles.actionBtn, styles.actionBtnDanger]}
                          onPress={() => handleDeleteServer(server)}
                          activeOpacity={0.82}
                        >
                          <Text
                            style={[
                              styles.actionBtnText,
                              styles.actionBtnDangerText,
                            ]}
                          >
                            Remove
                          </Text>
                        </TouchableOpacity>
                      </View>
                    </>
                  )}
                </TouchableOpacity>
              );
            })
          )}
        </View>

        <TouchableOpacity
          style={styles.addBtn}
          onPress={openCreateServer}
          activeOpacity={0.82}
        >
          <Ionicons name="add" size={16} color={Colors.textSecondary} />
          <Text style={styles.addBtnText}>Pair Server</Text>
        </TouchableOpacity>

        {/* Theme */}
        <Text style={styles.sectionLabel}>Theme</Text>
        <View style={styles.themeGrid}>
          {(Object.keys(TerminalThemes) as TerminalThemeName[]).map(
            (themeName) => {
              const active = terminalTheme === themeName;
              return (
                <TerminalThemeCard
                  key={themeName}
                  themeName={themeName}
                  active={active}
                  onPress={() => handleTerminalTheme(themeName)}
                />
              );
            },
          )}
        </View>

        <Text style={styles.version}>zen v0.1.0</Text>
      </ScrollView>

      {/* Unified Add/Edit Server modal */}
      <Modal
        visible={editorVisible}
        transparent
        animationType="fade"
        onRequestClose={closeEditor}
      >
        <KeyboardAvoidingView
          style={styles.modalRoot}
          behavior={Platform.OS === "ios" ? "padding" : undefined}
        >
          <TouchableOpacity
            style={styles.modalBackdrop}
            activeOpacity={1}
            onPress={closeEditor}
          />
          <View style={styles.modalContent}>
            <View style={styles.modalCard}>
              <Text style={styles.modalTitle}>
                {editingServerId ? "Edit Server" : "Pair Server"}
              </Text>

              {editingServer ? (
                <>
                  <Text style={styles.fieldLabel}>Name</Text>
                  <TextInput
                    style={styles.input}
                    value={draftName}
                    onChangeText={setDraftName}
                    placeholder="workstation"
                    placeholderTextColor="rgba(255,255,255,0.2)"
                    autoCapitalize="none"
                    autoCorrect={false}
                  />

                  <Text style={[styles.fieldLabel, { marginTop: 16 }]}>
                    Endpoint
                  </Text>
                  <TextInput
                    style={styles.input}
                    value={draftEndpoint}
                    onChangeText={setDraftEndpoint}
                    placeholder="wss://zen.example.com/ws"
                    placeholderTextColor="rgba(255,255,255,0.2)"
                    autoCapitalize="none"
                    autoCorrect={false}
                  />
                  <Text style={styles.fieldHint}>
                    This is the externally reachable WebSocket endpoint exposed
                    by your tunnel, reverse proxy, or private network.
                  </Text>

                  <View style={styles.identityCard}>
                    <Text style={styles.identityLabel}>Trusted Daemon</Text>
                    <Text style={styles.identityCode} numberOfLines={1}>
                      {editingServer.daemonId}
                    </Text>
                  </View>

                  <View style={styles.modalActions}>
                    <TouchableOpacity
                      style={styles.modalBtn}
                      onPress={closeEditor}
                      activeOpacity={0.82}
                    >
                      <Text style={styles.modalBtnText}>Cancel</Text>
                    </TouchableOpacity>
                    <TouchableOpacity
                      style={[styles.modalBtn, styles.modalBtnPrimary]}
                      onPress={() => void handleSaveServer()}
                      activeOpacity={0.82}
                    >
                      <Text
                        style={[
                          styles.modalBtnText,
                          styles.modalBtnPrimaryText,
                        ]}
                      >
                        Save
                      </Text>
                    </TouchableOpacity>
                  </View>
                </>
              ) : (
                <>
                  <Text style={styles.importLead}>
                    Paste the pairing link from zen-daemon, or scan its QR code.
                  </Text>

                  <Text style={styles.fieldLabel}>Pairing Link</Text>
                  <TextInput
                    style={[styles.input, styles.importInput]}
                    value={draftImportValue}
                    onChangeText={setDraftImportValue}
                    placeholder="zen://settings?p=..."
                    placeholderTextColor="rgba(255,255,255,0.2)"
                    autoCapitalize="none"
                    autoCorrect={false}
                    multiline
                    textAlignVertical="top"
                  />
                  <Text style={styles.fieldHint}>
                    You can also import a screenshot or photo of the QR.
                  </Text>

                  <View style={styles.modalActions}>
                    <TouchableOpacity
                      style={styles.modalBtn}
                      onPress={closeEditor}
                      activeOpacity={0.82}
                    >
                      <Text style={styles.modalBtnText}>Cancel</Text>
                    </TouchableOpacity>
                    <TouchableOpacity
                      style={[styles.modalBtn, styles.modalBtnPrimary]}
                      onPress={() => void handleImportDraft()}
                      activeOpacity={0.82}
                    >
                      <Text
                        style={[
                          styles.modalBtnText,
                          styles.modalBtnPrimaryText,
                        ]}
                      >
                        Import
                      </Text>
                    </TouchableOpacity>
                  </View>

                  <View style={styles.divider}>
                    <View style={styles.dividerLine} />
                    <Text style={styles.dividerText}>or</Text>
                    <View style={styles.dividerLine} />
                  </View>

                  <View style={styles.importRow}>
                    <TouchableOpacity
                      style={styles.importBtn}
                      onPress={() => void handlePasteImport()}
                      activeOpacity={0.82}
                    >
                      <Ionicons
                        name="clipboard-outline"
                        size={15}
                        color={Colors.textSecondary}
                      />
                      <Text style={styles.importBtnText}>Paste Link</Text>
                    </TouchableOpacity>
                    <TouchableOpacity
                      style={styles.importBtn}
                      onPress={openScanner}
                      activeOpacity={0.82}
                    >
                      <Ionicons
                        name="qr-code-outline"
                        size={15}
                        color={Colors.textSecondary}
                      />
                      <Text style={styles.importBtnText}>Scan QR</Text>
                    </TouchableOpacity>
                  </View>
                </>
              )}
            </View>
          </View>
        </KeyboardAvoidingView>
      </Modal>

      {/* QR Scanner */}
      <Modal
        visible={scannerVisible}
        animationType="slide"
        onRequestClose={closeScanner}
      >
        <View style={styles.scannerScreen}>
          <View style={styles.scannerHeader}>
            <Text style={styles.scannerTitle}>Scan Pairing QR</Text>
            <TouchableOpacity onPress={closeScanner} activeOpacity={0.82}>
              <Ionicons name="close" size={24} color={Colors.textPrimary} />
            </TouchableOpacity>
          </View>

          {!cameraPermission ? (
            <View style={styles.scannerNoticeCard}>
              <Text style={styles.scannerNoticeTitle}>Loading camera</Text>
            </View>
          ) : !cameraPermission.granted ? (
            <View style={styles.scannerNoticeCard}>
              <Text style={styles.scannerNoticeTitle}>
                Camera permission required
              </Text>
              <Text style={styles.scannerNoticeText}>
                Allow camera access to scan a zen-daemon pairing QR code.
              </Text>
              <TouchableOpacity
                style={styles.scannerPrimaryBtn}
                onPress={() => void requestCameraPermission()}
                activeOpacity={0.82}
              >
                <Text style={styles.scannerPrimaryBtnText}>
                  Grant Camera Access
                </Text>
              </TouchableOpacity>
            </View>
          ) : (
            <>
              <View style={styles.scannerViewport}>
                <CameraView
                  style={styles.scannerCamera}
                  facing="back"
                  barcodeScannerSettings={{ barcodeTypes: ["qr"] }}
                  onBarcodeScanned={
                    scannerLocked ? undefined : handleScanResult
                  }
                />
                <View pointerEvents="none" style={styles.scannerOverlay}>
                  <View style={styles.scannerMaskTop} />
                  <View style={styles.scannerMaskMiddle}>
                    <View style={styles.scannerMaskSide} />
                    <View style={styles.scannerFrame}>
                      <View style={styles.scannerFrameCornerTopLeft} />
                      <View style={styles.scannerFrameCornerTopRight} />
                      <View style={styles.scannerFrameCornerBottomLeft} />
                      <View style={styles.scannerFrameCornerBottomRight} />
                    </View>
                    <View style={styles.scannerMaskSide} />
                  </View>
                  <View style={styles.scannerMaskBottom} />
                </View>
              </View>

              <Text style={styles.scannerHelpText}>
                Scan the QR, or choose an image from this device.
              </Text>
            </>
          )}

          <View style={styles.scannerActions}>
            <TouchableOpacity
              style={[
                styles.scannerSecondaryBtn,
                scannerLocked && styles.scannerBtnDisabled,
              ]}
              onPress={() => void handlePickScannerImage()}
              disabled={scannerLocked}
              activeOpacity={0.82}
            >
              <Text style={styles.scannerSecondaryBtnText}>
                {scannerLocked ? "Reading Image..." : "Pick QR Image"}
              </Text>
            </TouchableOpacity>
            <TouchableOpacity
              style={styles.scannerPrimaryBtn}
              onPress={closeScanner}
              activeOpacity={0.82}
            >
              <Text style={styles.scannerPrimaryBtnText}>Done</Text>
            </TouchableOpacity>
          </View>
        </View>
      </Modal>
    </SafeAreaView>
  );
}

function ServerNoticeCard({
  icon,
  accent,
  title,
  detail,
  hint,
}: {
  icon: React.ComponentProps<typeof Ionicons>["name"];
  accent: string;
  title: string;
  detail: string;
  hint: string;
}) {
  return (
    <View style={[styles.noticeCard, { borderColor: accent }]}>
      <View style={styles.noticeHeader}>
        <Ionicons name={icon} size={15} color={accent} />
        <Text style={styles.noticeTitle}>{title}</Text>
      </View>
      <Text style={styles.noticeDetail}>{detail}</Text>
      <Text style={styles.noticeHint}>{hint}</Text>
    </View>
  );
}

function TerminalThemeCard({
  themeName,
  active,
  onPress,
}: {
  themeName: TerminalThemeName;
  active: boolean;
  onPress(): void;
}) {
  const theme = TerminalThemes[themeName];

  return (
    <TouchableOpacity
      style={[styles.themeCard, active && styles.themeCardActive]}
      onPress={onPress}
      activeOpacity={0.82}
    >
      {/* Mini terminal preview */}
      <View style={[styles.themePreview, { backgroundColor: theme.background }]}>
        {/* Traffic-light dots */}
        <View style={styles.themePreviewDots}>
          <View style={[styles.themePreviewDot, { backgroundColor: theme.red }]} />
          <View style={[styles.themePreviewDot, { backgroundColor: theme.yellow }]} />
          <View style={[styles.themePreviewDot, { backgroundColor: theme.green }]} />
        </View>
        {/* Fake terminal lines */}
        <View style={styles.themePreviewLines}>
          <View style={styles.themePreviewLine}>
            <Text style={[styles.themePreviewPrompt, { color: theme.green }]}>$ </Text>
            <Text style={[styles.themePreviewText, { color: theme.foreground }]}>zen</Text>
            <Text style={[styles.themePreviewText, { color: theme.blue, opacity: 0.8 }]}> --watch</Text>
          </View>
          <View style={styles.themePreviewLine}>
            <Text style={[styles.themePreviewText, { color: theme.cyan }]}>✓ </Text>
            <Text style={[styles.themePreviewText, { color: theme.foreground, opacity: 0.55 }]}>agents running</Text>
          </View>
          <View style={styles.themePreviewLine}>
            <Text style={[styles.themePreviewPrompt, { color: theme.foreground, opacity: 0.5 }]}>$ </Text>
            <View style={[styles.themePreviewCursor, { backgroundColor: theme.cursor }]} />
          </View>
        </View>
      </View>
      {/* Label row */}
      <View style={styles.themeCardLabel}>
        <Text style={[styles.themeCardName, active && styles.themeCardNameActive]}>
          {TerminalThemeLabels[themeName]}
        </Text>
        {active && <Ionicons name="checkmark" size={12} color={Colors.accent} />}
      </View>
    </TouchableOpacity>
  );
}

function connectionLabel(state: ConnectionState): string {
  switch (state) {
    case "connected":
      return "Connected";
    case "connecting":
      return "Connecting";
    case "offline":
      return "Offline";
  }
}

function connectionColor(state: ConnectionState): string {
  switch (state) {
    case "connected":
      return Colors.statusRunning;
    case "connecting":
      return "#E7B65C";
    case "offline":
      return "#65758A";
  }
}

function formatLatency(latencyMs: number): string {
  if (latencyMs >= 1000) {
    return `${(latencyMs / 1000).toFixed(1)}s`;
  }
  return `${latencyMs} ms`;
}

function latencyColor(latencyMs: number): string {
  if (latencyMs <= 120) {
    return Colors.statusRunning;
  }
  if (latencyMs <= 350) {
    return "#E7B65C";
  }
  return "#F09999";
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
  },
  header: {
    paddingHorizontal: 20,
    paddingTop: 8,
    paddingBottom: 12,
  },
  content: {
    paddingHorizontal: 20,
    paddingBottom: 40,
  },
  pageTitle: {
    color: Colors.textPrimary,
    fontSize: 22,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 1,
    opacity: 0.9,
  },

  // Section
  sectionHeader: {
    flexDirection: "row",
    alignItems: "baseline",
    justifyContent: "space-between",
    marginBottom: 10,
  },
  sectionLabel: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
    letterSpacing: 1.2,
    marginBottom: 10,
    marginTop: 20,
    opacity: 0.7,
  },
  sectionCount: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.terminalFont,
    opacity: 0.5,
  },

  // Server list
  serverList: {
    gap: 6,
  },
  serverCard: {
    borderRadius: 12,
    paddingHorizontal: 14,
    paddingVertical: 12,
    backgroundColor: "rgba(255,255,255,0.03)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.06)",
  },
  serverRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  statusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  serverInfo: {
    flex: 1,
  },
  serverStatus: {
    alignItems: "flex-end",
    justifyContent: "center",
    gap: 3,
  },
  serverName: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  serverUrl: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
    marginTop: 2,
    opacity: 0.6,
  },
  connectionLabel: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFont,
    opacity: 0.5,
  },
  connectionLabelActive: {
    color: Colors.statusRunning,
    opacity: 0.8,
  },
  latencyLabel: {
    fontSize: 11,
    fontFamily: Typography.terminalFont,
    opacity: 0.86,
  },
  noticeCard: {
    marginTop: 12,
    padding: 12,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    backgroundColor: "rgba(255,255,255,0.03)",
  },
  noticeHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  noticeTitle: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  noticeDetail: {
    marginTop: 8,
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
    opacity: 0.86,
  },
  noticeHint: {
    marginTop: 8,
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
    opacity: 0.62,
  },
  serverActions: {
    flexDirection: "row",
    gap: 8,
    marginTop: 12,
    paddingTop: 12,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: "rgba(255,255,255,0.06)",
  },
  actionBtn: {
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 8,
    backgroundColor: "rgba(255,255,255,0.05)",
  },
  actionBtnText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
    opacity: 0.8,
  },
  actionBtnDanger: {
    backgroundColor: "rgba(255,82,82,0.08)",
    marginLeft: "auto",
  },
  actionBtnDangerText: {
    color: "#F09999",
  },
  addBtn: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
    paddingVertical: 10,
    marginTop: 8,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    borderStyle: "dashed",
  },
  addBtnText: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
  },
  emptyCard: {
    paddingVertical: 20,
    alignItems: "center",
  },
  emptyText: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
    opacity: 0.5,
  },

  // Theme grid
  themeGrid: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 8,
  },
  themeCard: {
    width: "48.5%",
    borderRadius: 12,
    overflow: "hidden",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  themeCardActive: {
    borderWidth: 1.5,
    borderColor: Colors.accent,
  },
  themePreview: {
    height: 82,
    padding: 9,
  },
  themePreviewDots: {
    flexDirection: "row",
    gap: 5,
    marginBottom: 8,
  },
  themePreviewDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
    opacity: 0.75,
  },
  themePreviewLines: {
    gap: 4,
  },
  themePreviewLine: {
    flexDirection: "row",
    alignItems: "center",
  },
  themePreviewPrompt: {
    fontSize: 10,
    fontFamily: Typography.terminalFont,
  },
  themePreviewText: {
    fontSize: 10,
    fontFamily: Typography.terminalFont,
  },
  themePreviewCursor: {
    width: 6,
    height: 11,
    borderRadius: 1,
    opacity: 0.85,
  },
  themeCardLabel: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 10,
    paddingVertical: 7,
    backgroundColor: "rgba(255,255,255,0.03)",
  },
  themeCardName: {
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
    color: Colors.textSecondary,
  },
  themeCardNameActive: {
    color: Colors.textPrimary,
  },

  version: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFont,
    textAlign: "center",
    marginTop: 40,
    opacity: 0.3,
  },

  // Modal
  modalRoot: {
    flex: 1,
    paddingHorizontal: 24,
    paddingVertical: 24,
  },
  modalContent: {
    flex: 1,
    justifyContent: "center",
  },
  modalBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: "rgba(0,0,0,0.7)",
  },
  modalCard: {
    borderRadius: 16,
    padding: 20,
    maxWidth: 520,
    width: "100%",
    alignSelf: "center",
    backgroundColor: "#141418",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  modalTitle: {
    color: Colors.textPrimary,
    fontSize: 17,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 20,
  },
  importLead: {
    color: Colors.textSecondary,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
    marginBottom: 18,
    opacity: 0.82,
  },
  fieldLabel: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
    marginBottom: 6,
    opacity: 0.6,
  },
  input: {
    backgroundColor: "rgba(255,255,255,0.04)",
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 10,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.terminalFont,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  importInput: {
    minHeight: 116,
    paddingTop: 12,
  },
  fieldHint: {
    marginTop: 8,
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
    opacity: 0.65,
  },
  identityCard: {
    marginTop: 16,
    borderRadius: 10,
    padding: 12,
    backgroundColor: "rgba(255,255,255,0.03)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  identityLabel: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 8,
    opacity: 0.7,
  },
  identityCode: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.terminalFont,
    opacity: 0.86,
  },
  modalActions: {
    flexDirection: "row",
    justifyContent: "flex-end",
    gap: 10,
    marginTop: 24,
  },
  modalBtn: {
    minWidth: 70,
    height: 36,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.05)",
  },
  modalBtnPrimary: {
    backgroundColor: Colors.accent,
  },
  modalBtnText: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  modalBtnPrimaryText: {
    color: Colors.bgPrimary,
  },
  divider: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    marginTop: 20,
    marginBottom: 16,
  },
  dividerLine: {
    flex: 1,
    height: StyleSheet.hairlineWidth,
    backgroundColor: "rgba(255,255,255,0.08)",
  },
  dividerText: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
    opacity: 0.5,
  },
  importRow: {
    flexDirection: "row",
    gap: 10,
  },
  importBtn: {
    flex: 1,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
    height: 40,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    backgroundColor: "rgba(255,255,255,0.03)",
  },
  importBtnText: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },

  // Scanner
  scannerScreen: {
    flex: 1,
    backgroundColor: "#0A0C10",
    paddingTop: 64,
    paddingHorizontal: 20,
    paddingBottom: 28,
  },
  scannerHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    marginBottom: 18,
  },
  scannerTitle: {
    color: Colors.textPrimary,
    fontSize: 20,
    fontFamily: Typography.uiFontMedium,
  },
  scannerViewport: {
    borderRadius: 24,
    overflow: "hidden",
    backgroundColor: "#050608",
    minHeight: 440,
  },
  scannerCamera: {
    flex: 1,
    minHeight: 440,
  },
  scannerOverlay: {
    ...StyleSheet.absoluteFillObject,
  },
  scannerMaskTop: {
    flex: 1,
    backgroundColor: "rgba(5,8,12,0.52)",
  },
  scannerMaskMiddle: {
    height: 240,
    flexDirection: "row",
  },
  scannerMaskSide: {
    flex: 1,
    backgroundColor: "rgba(5,8,12,0.52)",
  },
  scannerFrame: {
    width: 240,
    borderRadius: 20,
    borderWidth: 1,
    borderColor: "rgba(255,255,255,0.18)",
  },
  scannerMaskBottom: {
    flex: 1,
    backgroundColor: "rgba(5,8,12,0.52)",
  },
  scannerFrameCornerTopLeft: {
    position: "absolute",
    top: -1,
    left: -1,
    width: 32,
    height: 32,
    borderTopWidth: 4,
    borderLeftWidth: 4,
    borderColor: Colors.accent,
    borderTopLeftRadius: 20,
  },
  scannerFrameCornerTopRight: {
    position: "absolute",
    top: -1,
    right: -1,
    width: 32,
    height: 32,
    borderTopWidth: 4,
    borderRightWidth: 4,
    borderColor: Colors.accent,
    borderTopRightRadius: 20,
  },
  scannerFrameCornerBottomLeft: {
    position: "absolute",
    bottom: -1,
    left: -1,
    width: 32,
    height: 32,
    borderBottomWidth: 4,
    borderLeftWidth: 4,
    borderColor: Colors.accent,
    borderBottomLeftRadius: 20,
  },
  scannerFrameCornerBottomRight: {
    position: "absolute",
    bottom: -1,
    right: -1,
    width: 32,
    height: 32,
    borderBottomWidth: 4,
    borderRightWidth: 4,
    borderColor: Colors.accent,
    borderBottomRightRadius: 20,
  },
  scannerHelpText: {
    marginTop: 18,
    color: Colors.textSecondary,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
    textAlign: "center",
    opacity: 0.8,
  },
  scannerActions: {
    flexDirection: "row",
    gap: 10,
    marginTop: 20,
  },
  scannerNoticeCard: {
    marginTop: 24,
    borderRadius: 18,
    padding: 20,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    alignItems: "center",
  },
  scannerNoticeTitle: {
    color: Colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
  },
  scannerNoticeText: {
    marginTop: 8,
    color: Colors.textSecondary,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
    textAlign: "center",
    opacity: 0.8,
  },
  scannerPrimaryBtn: {
    flex: 1,
    marginTop: 16,
    minHeight: 44,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: Colors.accent,
    paddingHorizontal: 16,
  },
  scannerPrimaryBtnText: {
    color: Colors.bgPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  scannerSecondaryBtn: {
    flex: 1,
    marginTop: 16,
    minHeight: 44,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.06)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    paddingHorizontal: 16,
  },
  scannerBtnDisabled: {
    opacity: 0.45,
  },
  scannerSecondaryBtnText: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
});
