import React, { useEffect, useMemo, useState } from "react";
import {
  Alert,
  LayoutAnimation,
  Modal,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { useFocusEffect, useLocalSearchParams } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
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
import {
  Colors,
  Radii,
  Typography,
  UiTextMetrics,
  uiLineHeight,
  useAppColors,
  useAppTheme,
  shadow,
} from "../../constants/tokens";
import type { ResolvedZenTheme } from "../../theme";
import { createThemedSurfaces } from "../../constants/themedSurfaces";
import { importConnection } from "../../services/importConnection";
import { wsClient } from "../../services/websocket";
import { ConnectionState, useAgentServerSummary } from "../../store/agents";
import * as Storage from "../../services/storage";
import { connectionIssueAccent } from "../../services/connectionIssue";
import { AnimatedPressable } from "../../components/ui/AnimatedPressable";
import { RisingSheet } from "../../components/ui/RisingSheet";
import { TelegramSettingsRow } from "../../components/ui/TelegramSettingsRow";

const QR_BARCODE_TYPES: BarcodeType[] = ["qr"];

export default function SettingsScreen() {
  const {
    agentCountsByServer,
    dispatch,
    hydratedServers,
    serverConnections,
    serverConnectionIssues,
    serverLatencyById,
  } = useAgentServerSummary();
  const colors = useAppColors();
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);
  const params = useLocalSearchParams<{
    addServer?: string;
    refresh?: string;
  }>();
  const [servers, setServers] = useState<Storage.StoredServer[]>([]);
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
        (server) => serverConnections[server.id] === "connected",
      ).length,
    [servers, serverConnections],
  );
  const editingServer = useMemo(
    () => servers.find((server) => server.id === editingServerId) || null,
    [editingServerId, servers],
  );

  useFocusEffect(
    React.useCallback(() => {
      let cancelled = false;

      (async () => {
        const savedServers = await Storage.getServers();
        if (cancelled) return;

        setServers(savedServers);
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
      ? serverConnections[editingServerId]
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
          "Use a full ws://, wss://, http://, or https:// URL that points at zen.",
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
          "Could not parse the pairing link. Import the zen:// link or QR printed by zen.",
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

  const toggleServerExpand = (serverId: string) => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    LayoutAnimation.configureNext(
      LayoutAnimation.create(
        200,
        LayoutAnimation.Types.easeInEaseOut,
        LayoutAnimation.Properties.opacity,
      ),
    );
    setExpandedServer((prev) => (prev === serverId ? null : serverId));
  };

  const handleImportDraft = async () => {
    let rawValue = draftImportValue.trim();
    if (!rawValue) {
      rawValue = (await Clipboard.getStringAsync()).trim();
    }
    if (!rawValue) {
      Alert.alert(
        "Pairing link required",
        "Paste the pairing link printed by zen, or scan its QR code.",
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
        <View style={styles.profileAvatar}>
          <Ionicons name="person" size={34} color={colors.textPrimary} />
        </View>
        <Text style={styles.pageTitle}>Settings</Text>
        <Text style={styles.pageSubtitle}>Pair and manage daemon endpoints</Text>
      </View>
      <ScrollView
        style={styles.scrollView}
        contentContainerStyle={styles.content}
        showsVerticalScrollIndicator={false}
      >
          <View style={styles.sectionHeader}>
            <Text style={[styles.sectionLabel, { marginTop: 0 }]}>Servers</Text>
            {servers.length > 0 ? (
              <Text style={styles.sectionCount}>
                {connectedCount} of {servers.length} connected
              </Text>
            ) : null}
          </View>

          <View style={styles.serverList}>
            {servers.length === 0 ? (
              <View style={styles.emptyCard}>
                <Text style={styles.emptyText}>No paired daemons yet</Text>
                <Text style={styles.emptyHint}>Pair your first server below</Text>
              </View>
            ) : (
              servers.map((server) => {
                const connectionState =
                  serverConnections[server.id] || "offline";
                const latencySample = serverLatencyById[server.id];
                const connectionIssue =
                  serverConnectionIssues[server.id] || null;
                const expanded = expandedServer === server.id;
                const agentCount = agentCountsByServer[server.id] || 0;
                const hydrated = Boolean(hydratedServers[server.id]);
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
                  <View key={server.id} style={styles.serverCard}>
                    <AnimatedPressable
                      style={styles.serverHeaderButton}
                      preset="card"
                      scale={0.99}
                      onPress={() => toggleServerExpand(server.id)}
                    >
                      <View style={styles.serverRow}>
                        <View
                          style={[
                            styles.statusDot,
                            { backgroundColor: connectionColor(connectionState, colors) },
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
                                  color: latencyColor(latencySample.latencyMs, colors),
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
                    </AnimatedPressable>

                    {expanded && (
                      <View style={styles.serverExpandedContent}>
                        {connectionIssue ? (
                          <ServerNoticeCard
                            icon="alert-circle-outline"
                            accent={connectionIssueAccent(connectionIssue, colors)}
                            title={connectionIssue.title}
                            detail={connectionIssue.detail}
                            hint={connectionIssue.hint}
                          />
                        ) : null}

                        {waitingForAgents ? (
                          <ServerNoticeCard
                            icon="information-circle-outline"
                            accent={colors.accent}
                            title={
                              hydrated
                                ? "Connected, no active agents yet"
                                : "Connected, waiting for agent data"
                            }
                            detail={
                              hydrated
                                ? "Zen is connected to this daemon, but it has not reported any live agents yet."
                                : "Zen is connected to this daemon and waiting for the first agent list to arrive."
                            }
                            hint="Start Claude or Codex on that machine, or verify the watcher/tmux bridge is forwarding terminals."
                          />
                        ) : null}

                        <View style={styles.serverActions}>
                          <AnimatedPressable
                            style={styles.actionBtn}
                            preset="press"
                            scale={0.95}
                            onPress={() => {
                              void (
                                connectionState === "connected"
                                  ? disconnectServer(server.id)
                                  : connectServer(server)
                              );
                              void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                            }}
                          >
                            <Text style={styles.actionBtnText}>
                              {actionLabel}
                            </Text>
                          </AnimatedPressable>
                          <AnimatedPressable
                            style={styles.actionBtn}
                            preset="press"
                            scale={0.95}
                            onPress={() => {
                              openEditServer(server);
                              void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                            }}
                          >
                            <Text style={styles.actionBtnText}>Edit</Text>
                          </AnimatedPressable>
                          <AnimatedPressable
                            style={[styles.actionBtn, styles.actionBtnDanger]}
                            preset="press"
                            scale={0.95}
                            onPress={() => {
                              handleDeleteServer(server);
                              void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
                            }}
                          >
                            <Text style={[styles.actionBtnText, styles.actionBtnDangerText]}>
                              Remove
                            </Text>
                          </AnimatedPressable>
                        </View>
                      </View>
                    )}
                  </View>
                );
              })
            )}
            <TelegramSettingsRow
              icon="add-circle-outline"
              iconColor={colors.accent}
              title="Add Server"
              subtitle="Pair via link or QR code"
              onPress={() => {
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                openCreateServer();
              }}
              isLast
            />
          </View>

          <Text style={styles.version}>Zen v0.1.0</Text>
      </ScrollView>

      {/* Unified Add/Edit Server modal */}
      <RisingSheet
        visible={editorVisible}
        onClose={closeEditor}
        cardStyle={styles.modalCard}
        avoidKeyboard
      >
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
              placeholderTextColor={colors.textSecondary}
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
              placeholderTextColor={colors.textSecondary}
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
              <AnimatedPressable style={styles.modalBtn} preset="press" scale={0.94} onPress={closeEditor}>
                <Text style={styles.modalBtnText}>Cancel</Text>
              </AnimatedPressable>
              <AnimatedPressable
                style={[styles.modalBtn, styles.modalBtnPrimary]}
                preset="press"
                scale={0.94}
                onPress={() => {
                  Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                  void handleSaveServer();
                }}
              >
                <Text style={[styles.modalBtnText, styles.modalBtnPrimaryText]}>Save</Text>
              </AnimatedPressable>
            </View>
          </>
        ) : (
          <>
            <Text style={styles.importLead}>
              Paste the pairing link from zen, or scan its QR code.
            </Text>

            <Text style={styles.fieldLabel}>Pairing Link</Text>
            <TextInput
              style={[styles.input, styles.importInput]}
              value={draftImportValue}
              onChangeText={setDraftImportValue}
              placeholder="zen://settings?p=..."
              placeholderTextColor={colors.textSecondary}
              autoCapitalize="none"
              autoCorrect={false}
              multiline
              textAlignVertical="top"
            />
            <Text style={styles.fieldHint}>
              You can also import a screenshot or photo of the QR.
            </Text>

            <View style={styles.modalActions}>
              <AnimatedPressable style={styles.modalBtn} preset="press" scale={0.94} onPress={closeEditor}>
                <Text style={styles.modalBtnText}>Cancel</Text>
              </AnimatedPressable>
              <AnimatedPressable
                style={[styles.modalBtn, styles.modalBtnPrimary]}
                preset="press"
                scale={0.94}
                onPress={() => {
                  Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                  void handleImportDraft();
                }}
              >
                <Text style={[styles.modalBtnText, styles.modalBtnPrimaryText]}>Import</Text>
              </AnimatedPressable>
            </View>

            <AnimatedPressable
              style={styles.scanQrBtn}
              preset="press"
              scale={0.98}
              onPress={() => {
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                openScanner();
              }}
            >
              <Ionicons name="qr-code-outline" size={18} color={colors.accent} />
              <Text style={styles.scanQrBtnText}>Scan QR Code</Text>
            </AnimatedPressable>
          </>
        )}
      </RisingSheet>

      {/* QR Scanner */}
      <Modal
        visible={scannerVisible}
        animationType="slide"
        onRequestClose={closeScanner}
      >
        <View style={styles.scannerScreen}>
          <View style={styles.scannerHeader}>
            <Text style={styles.scannerTitle}>Scan Pairing QR</Text>
            <AnimatedPressable
              preset="press"
              scale={0.9}
              onPress={() => {
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                closeScanner();
              }}
            >
              <Ionicons name="close" size={24} color={colors.textPrimary} />
            </AnimatedPressable>
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
                Allow camera access to scan a zen pairing QR code.
              </Text>
              <AnimatedPressable
                style={styles.scannerPrimaryBtn}
                preset="press"
                scale={0.96}
                onPress={() => void requestCameraPermission()}
              >
                <Text style={styles.scannerPrimaryBtnText}>
                  Grant Camera Access
                </Text>
              </AnimatedPressable>
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
            <AnimatedPressable
              style={[styles.scannerSecondaryBtn, scannerLocked && styles.scannerBtnDisabled]}
              preset="press"
              scale={0.96}
              disabled={scannerLocked}
              onPress={() => void handlePickScannerImage()}
            >
              <Text style={styles.scannerSecondaryBtnText}>
                {scannerLocked ? "Reading Image..." : "Pick QR Image"}
              </Text>
            </AnimatedPressable>
            <AnimatedPressable
              style={styles.scannerPrimaryBtn}
              preset="press"
              scale={0.96}
              onPress={() => {
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                closeScanner();
              }}
            >
              <Text style={styles.scannerPrimaryBtnText}>Done</Text>
            </AnimatedPressable>
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
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);

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

function connectionColor(state: ConnectionState, colors: typeof Colors = Colors): string {
  switch (state) {
    case "connected":
      return colors.statusRunning;
    case "connecting":
      return colors.warning;
    case "offline":
      return colors.disabledText;
  }
}

function formatLatency(latencyMs: number): string {
  if (latencyMs >= 1000) {
    return `${(latencyMs / 1000).toFixed(1)}s`;
  }
  return `${latencyMs} ms`;
}

function latencyColor(latencyMs: number, colors: typeof Colors = Colors): string {
  if (latencyMs <= 120) {
    return colors.statusRunning;
  }
  if (latencyMs <= 350) {
    return colors.warning;
  }
  return colors.dangerText;
}

function createStyles(theme: ResolvedZenTheme) {
  const colors = theme.colors;
  const {
    surface: themedSurface,
    border: themedBorder,
    sectionLabel,
  } = createThemedSurfaces(theme);

  return StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.bgPrimary,
  },
  header: {
    paddingHorizontal: 20,
    paddingTop: 18,
    paddingBottom: 20,
    alignItems: "center",
    backgroundColor: colors.bgPrimary,
    zIndex: 2,
  },
  scrollView: {
    flex: 1,
  },
  content: {
    paddingHorizontal: 20,
    paddingTop: 4,
    paddingBottom: 110,
  },
  profileAvatar: {
    width: 82,
    height: 82,
    borderRadius: 41,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: colors.bgElevated,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    marginBottom: 10,
  },
  pageTitle: {
    ...UiTextMetrics,
    color: colors.textPrimary,
    fontSize: 30,
    lineHeight: uiLineHeight(30),
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 0,
  },
  pageSubtitle: {
    ...UiTextMetrics,
    marginTop: 3,
    color: colors.textSecondary,
    fontSize: 13,
    lineHeight: uiLineHeight(13),
    fontFamily: Typography.uiFont,
  },

  // Section
  sectionHeader: {
    flexDirection: "row",
    alignItems: "baseline",
    justifyContent: "space-between",
    marginBottom: 12,
    marginTop: 4,
  },
  sectionLabel: {
    color: sectionLabel,
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
    letterSpacing: 1.2,
    marginBottom: 10,
    marginTop: 20,
  },
  sectionCount: {
    color: colors.textTertiary,
    fontSize: 11.5,
    fontFamily: Typography.terminalFont,
  },

  // Server list
  serverList: {
    gap: 0,
    overflow: "hidden",
    borderRadius: Radii.lg,
    backgroundColor: colors.bgSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.borderSubtle,
  },
  serverCard: {
    borderRadius: 0,
    backgroundColor: colors.bgSurface,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.borderSubtle,
  },
  serverHeaderButton: {
    paddingHorizontal: 16,
    paddingVertical: 14,
    backgroundColor: colors.bgSurface,
  },
  serverExpandedContent: {
    paddingHorizontal: 16,
    paddingBottom: 14,
  },
  serverRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 11,
  },
  statusDot: {
    width: 8,
    height: 8,
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
    color: colors.textPrimary,
    fontSize: 15,
    fontFamily: Typography.uiFontMedium,
  },
  serverUrl: {
    color: colors.textTertiary,
    fontSize: 11.5,
    fontFamily: Typography.terminalFont,
    marginTop: 3,
  },
  connectionLabel: {
    color: colors.textTertiary,
    fontSize: 11,
    fontFamily: Typography.uiFont,
  },
  connectionLabelActive: {
    color: colors.statusRunning,
  },
  latencyLabel: {
    fontSize: 11,
    fontFamily: Typography.terminalFont,
  },
  noticeCard: {
    marginTop: 12,
    padding: 14,
    borderRadius: Radii.sm,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: themedBorder,
    backgroundColor: themedSurface,
  },
  noticeHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  noticeTitle: {
    flex: 1,
    color: colors.textPrimary,
    fontSize: 13.5,
    fontFamily: Typography.uiFontMedium,
  },
  noticeDetail: {
    marginTop: 8,
    color: colors.textSecondary,
    fontSize: 12.5,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  noticeHint: {
    marginTop: 8,
    color: colors.textTertiary,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  serverActions: {
    flexDirection: "row",
    gap: 8,
    marginTop: 14,
    paddingTop: 14,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.borderSubtle,
  },
  actionBtn: {
    paddingHorizontal: 14,
    paddingVertical: 8,
    borderRadius: Radii.xs,
    backgroundColor: colors.surfacePressed,
  },
  actionBtnText: {
    color: colors.textPrimary,
    fontSize: 12.5,
    fontFamily: Typography.uiFontMedium,
  },
  actionBtnDanger: {
    backgroundColor: colors.dangerSoft,
    marginLeft: "auto",
  },
  actionBtnDangerText: {
    color: colors.dangerText,
  },
  emptyCard: {
    paddingVertical: 28,
    alignItems: "center",
    gap: 4,
  },
  emptyText: {
    color: colors.textTertiary,
    fontSize: 13.5,
    fontFamily: Typography.uiFont,
  },
  emptyHint: {
    color: colors.textTertiary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
    opacity: 0.75,
  },

  version: {
    color: colors.textTertiary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
    textAlign: "center",
    marginTop: 44,
  },

  // Modal / editor
  modalCard: {
    borderRadius: Radii.lg,
    padding: 22,
    maxWidth: 480,
    width: "100%",
    alignSelf: "center",
    backgroundColor: colors.modalSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    ...shadow("float", colors.shadowColor),
  },
  modalTitle: {
    color: colors.textPrimary,
    fontSize: 18,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 20,
  },
  importLead: {
    color: colors.textSecondary,
    fontSize: 13.5,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
    marginBottom: 18,
  },
  fieldLabel: {
    color: colors.textTertiary,
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 0.4,
    textTransform: "uppercase",
    marginBottom: 7,
  },
  input: {
    backgroundColor: colors.inputBackground,
    borderRadius: Radii.sm,
    paddingHorizontal: 14,
    paddingVertical: 12,
    color: colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.terminalFont,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
  },
  importInput: {
    minHeight: 120,
    paddingTop: 12,
  },
  fieldHint: {
    marginTop: 8,
    color: colors.textTertiary,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  identityCard: {
    marginTop: 16,
    borderRadius: Radii.sm,
    padding: 14,
    backgroundColor: colors.surfaceSubtle,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
  },
  identityLabel: {
    color: colors.textTertiary,
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 8,
    letterSpacing: 0.4,
    textTransform: "uppercase",
  },
  identityCode: {
    color: colors.textPrimary,
    fontSize: 12.5,
    fontFamily: Typography.terminalFont,
  },
  modalActions: {
    flexDirection: "row",
    justifyContent: "flex-end",
    gap: 10,
    marginTop: 24,
  },
  modalBtn: {
    minWidth: 76,
    height: 40,
    borderRadius: Radii.sm,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: colors.surfacePressed,
  },
  modalBtnPrimary: {
    backgroundColor: colors.accent,
  },
  modalBtnText: {
    color: colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  modalBtnPrimaryText: {
    color: colors.textOnAccent,
  },
  scanQrBtn: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
    height: 44,
    marginTop: 14,
    borderRadius: Radii.sm,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    backgroundColor: colors.surfaceSubtle,
  },
  scanQrBtnText: {
    color: colors.accent,
    fontSize: 14,
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
    color: colors.textPrimary,
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
    ...StyleSheet.absoluteFill,
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
    borderColor: colors.accent,
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
    borderColor: colors.accent,
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
    borderColor: colors.accent,
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
    borderColor: colors.accent,
    borderBottomRightRadius: 20,
  },
  scannerHelpText: {
    marginTop: 18,
    color: colors.textSecondary,
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
    padding: 24,
    backgroundColor: colors.surfaceSubtle,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    alignItems: "center",
  },
  scannerNoticeTitle: {
    color: colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
  },
  scannerNoticeText: {
    marginTop: 8,
    color: colors.textSecondary,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
    textAlign: "center",
    opacity: 0.8,
  },
  scannerPrimaryBtn: {
    flex: 1,
    marginTop: 16,
    minHeight: 46,
    borderRadius: Radii.sm,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: colors.accent,
    paddingHorizontal: 16,
  },
  scannerPrimaryBtnText: {
    color: colors.textOnAccent,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  scannerSecondaryBtn: {
    flex: 1,
    marginTop: 16,
    minHeight: 46,
    borderRadius: Radii.sm,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: colors.surfacePressed,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    paddingHorizontal: 16,
  },
  scannerBtnDisabled: {
    opacity: 0.45,
  },
  scannerSecondaryBtnText: {
    color: colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  });
}
