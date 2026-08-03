import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Alert,
  LayoutAnimation,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
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
  TypeScale,
  Typography,
  UiTextMetrics,
  useAppColors,
  useAppTheme,
  shadow,
} from "../constants/tokens";
import { useZenTheme, type ResolvedZenTheme } from "../theme";
import { ZEN_DARK_APP_COLORS } from "../theme/primitives";
import { appVersion } from "../constants/appVersion";
import { importConnection } from "../services/importConnection";
import {
  closePairPresentation,
  completePairImport,
  createClosedPairPresentation,
  lockPairScanner,
  openPairEditor,
  openPairScanner,
  resolvePairPresentationDismiss,
  returnToPairEditor,
  unlockPairScanner,
  type PairPresentationState,
} from "../services/pairPresentation";
import {
  attemptDismissPairScanner,
  createPairScanClaim,
  isPairScanClaimHeld,
  releasePairScan,
  tryClaimPairScan,
} from "../services/pairScanClaim";
import { wsClient } from "../services/websocket";
import {
  ConnectionState,
  countAgentsByServer,
  useAgentList,
  useAgentServerSummary,
} from "../store/agents";
import * as Storage from "../services/storage";
import { connectionIssueAccent } from "../services/connectionIssue";
import { AnimatedPressable } from "../components/ui/AnimatedPressable";
import { RisingSheet } from "../components/ui/RisingSheet";
import { cancelCalendarNotifications } from "../services/calendarNotifications";
import { useCurrentServer } from "../store/currentServer";

const QR_BARCODE_TYPES: BarcodeType[] = ["qr"];
const SCANNER_COLORS = ZEN_DARK_APP_COLORS;
const THEME_CHOICES = [
  { label: "System", value: "system", icon: "phone-portrait-outline" },
  { label: "Light", value: "classic-light", icon: "sunny-outline" },
  { label: "Dark", value: "classic-dark", icon: "moon-outline" },
] as const;

export default function SettingsScreen() {
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const agents = useAgentList();
  const {
    currentServerId,
    refreshServers: refreshCurrentServers,
    switchCurrentServer,
  } = useCurrentServer();
  const {
    dispatch,
    hydratedServers,
    serverConnections,
    serverConnectionIssues,
    serverLatencyById,
  } = useAgentServerSummary();
  const agentCounts = useMemo(() => countAgentsByServer(agents), [agents]);
  const colors = useAppColors();
  const { preference, setPreference } = useZenTheme();
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);
  const params = useLocalSearchParams<{
    addServer?: string;
    refresh?: string;
    pairingRequired?: string;
  }>();
  const [servers, setServers] = useState<Storage.StoredServer[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [pairPresentation, setPairPresentation] =
    useState<PairPresentationState>(createClosedPairPresentation);
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
  const [cameraMountError, setCameraMountError] = useState<string | null>(null);
  const scanClaimRef = useRef(createPairScanClaim());

  const releaseScanClaim = useCallback(() => {
    releasePairScan(scanClaimRef.current);
    setPairPresentation((current) => unlockPairScanner(current));
  }, []);

  const tryAcquireScanClaim = useCallback(() => {
    if (!tryClaimPairScan(scanClaimRef.current)) {
      return false;
    }
    setPairPresentation((current) => lockPairScanner(current));
    return true;
  }, []);

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

  const refreshServers = async (preferredServerId?: string) => {
    setServers(await Storage.getServers());
    await refreshCurrentServers(preferredServerId);
  };

  const connectServer = async (server: Storage.StoredServer) => {
    await Storage.setServerAutoConnect(server.id, true);
    if (server.id !== currentServerId) {
      await switchCurrentServer(server.id);
      return;
    }
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
    setCameraMountError(null);
    setPairPresentation(openPairEditor());
  };

  const openEditServer = (server: Storage.StoredServer) => {
    setEditingServerId(server.id);
    setDraftName(server.name);
    setDraftEndpoint(server.url);
    setDraftImportValue("");
    setCameraMountError(null);
    setPairPresentation(openPairEditor());
  };

  const closeEditor = () => {
    // Never release an in-flight import claim from editor teardown.
    if (isPairScanClaimHeld(scanClaimRef.current)) {
      return;
    }
    setPairPresentation(closePairPresentation());
    setEditingServerId(null);
    setDraftName("");
    setDraftEndpoint("");
    setDraftImportValue("");
    setCameraMountError(null);
  };

  const openScanner = () => {
    if (isPairScanClaimHeld(scanClaimRef.current)) {
      return;
    }
    releasePairScan(scanClaimRef.current);
    setCameraMountError(null);
    setPairPresentation((current) => openPairScanner(current));
  };

  const closeScanner = () => {
    // Ref is authority: Done/backdrop/system-back must not dismiss or release
    // while import claim is held — import finally owns release.
    if (attemptDismissPairScanner(scanClaimRef.current) === "blocked") {
      return;
    }
    setCameraMountError(null);
    setPairPresentation((current) => returnToPairEditor(current));
  };

  const handlePairPresentationDismiss = () => {
    // Check claim before reading React mode — ref wins over lagging state.
    if (attemptDismissPairScanner(scanClaimRef.current) === "blocked") {
      return;
    }
    if (
      resolvePairPresentationDismiss(pairPresentation.mode) ===
      "return-to-editor"
    ) {
      setCameraMountError(null);
      setPairPresentation((current) => returnToPairEditor(current));
      return;
    }
    closeEditor();
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
        transportKind: editingServer.transportKind,
        transportPin: editingServer.transportPin,
        linkRouteId: editingServer.linkRouteId,
        transportCandidates: editingServer.transportCandidates,
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

  const importServer = async (rawValue: string) => {
    try {
      const savedServer = await importConnection(rawValue, {
        onImported: async (importedServer) => {
          await refreshServers(importedServer.id);
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

      // Paste, image, and camera share one success path: release the single Modal.
      setPairPresentation(completePairImport());
      setEditingServerId(null);
      setDraftName("");
      setDraftEndpoint("");
      setDraftImportValue("");
      setCameraMountError(null);
      if (params.pairingRequired === "1") {
        router.dismissAll();
        router.replace("/");
      }
      return true;
    } catch (error: any) {
      Alert.alert(
        "Pairing failed",
        error?.message || "Could not pair with that daemon.",
      );
      return false;
    } finally {
      releaseScanClaim();
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
          await cancelCalendarNotifications(server.id);
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
    // Sync claim gate: same-frame camera callbacks cannot both enter import.
    if (!tryAcquireScanClaim()) return;
    await importServer(data || "");
  };

  const handlePickScannerImage = async () => {
    // Claim before DocumentPicker so live onBarcodeScanned is disabled while
    // the system picker is open (UI lock + atomic ref owner).
    if (!tryAcquireScanClaim()) return;

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

      const matches = await scanFromURLAsync(asset.uri, QR_BARCODE_TYPES);
      const qrMatch = matches.find((item) => (item.data || "").trim());
      if (!qrMatch?.data) {
        Alert.alert(
          "QR not found",
          "No QR code was detected in that image. Use a tighter crop with the QR filling most of the frame.",
        );
        return;
      }

      await importServer(qrMatch.data);
    } catch (error: any) {
      Alert.alert(
        "Image scan failed",
        error?.message || "Could not read a QR code from that image.",
      );
    } finally {
      releaseScanClaim();
    }
  };

  if (!loaded) return null;

  return (
    <SafeAreaView style={styles.container} edges={[]}>
      <ScrollView
        style={styles.scrollView}
        contentContainerStyle={[
          styles.content,
          { paddingBottom: Math.max(insets.bottom, 20) + 12 },
        ]}
        showsVerticalScrollIndicator={false}
      >
        <View style={styles.contentInner}>
          <View style={styles.sectionHeader}>
            <Text style={styles.sectionLabel} accessibilityRole="header">
              Connections
            </Text>
          </View>

          <View style={styles.serverList}>
            {servers.length === 0 ? (
              <View style={styles.emptyCard}>
                <Text style={styles.emptyText}>No paired daemons yet</Text>
                <Text style={styles.emptyHint}>
                  Pair your first server below
                </Text>
              </View>
            ) : (
              servers.map((server) => {
                const current = server.id === currentServerId;
                const connectionState =
                  serverConnections[server.id] || "offline";
                const latencySample = serverLatencyById[server.id];
                const connectionIssue =
                  serverConnectionIssues[server.id] || null;
                const expanded = expandedServer === server.id;
                const agentCount = agentCounts[server.id] || 0;
                const hydrated = Boolean(hydratedServers[server.id]);
                const waitingForAgents =
                  connectionState === "connected" &&
                  (!hydrated || agentCount === 0);
                const actionLabel = !current
                  ? "Use"
                  : connectionState === "connected"
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
                      accessibilityRole="button"
                      accessibilityLabel={`${server.name}, ${connectionLabel(connectionState)}${
                        connectionState === "connected" && latencySample
                          ? `, ${formatLatency(latencySample.latencyMs)} latency`
                          : ""
                      }, ${server.url}`}
                      accessibilityHint={
                        expanded
                          ? "Hide server details"
                          : "Show server details and actions"
                      }
                      accessibilityState={{ expanded }}
                      onPress={() => toggleServerExpand(server.id)}
                    >
                      <View style={styles.serverRow}>
                        <View
                          style={[
                            styles.statusDot,
                            {
                              backgroundColor: connectionColor(
                                connectionState,
                                colors,
                              ),
                            },
                          ]}
                        />
                        <View style={styles.serverInfo}>
                          <Text style={styles.serverName} numberOfLines={1}>
                            {server.name}
                          </Text>
                          <Text style={styles.serverUrl} numberOfLines={1}>
                            {server.transportKind === "link"
                              ? "Zen Link · route selected automatically"
                              : server.url}
                          </Text>
                          <View style={styles.serverStatus}>
                            <Text
                              style={[
                                styles.connectionLabel,
                                connectionState === "connected" &&
                                  styles.connectionLabelActive,
                              ]}
                            >
                              {connectionLabel(connectionState)}
                            </Text>
                            {connectionState === "connected" &&
                            latencySample ? (
                              <>
                                <Text style={styles.metadataSeparator}>/</Text>
                                <Text
                                  style={[
                                    styles.latencyLabel,
                                    {
                                      color: latencyColor(
                                        latencySample.latencyMs,
                                        colors,
                                      ),
                                    },
                                  ]}
                                >
                                  {formatLatency(latencySample.latencyMs)}
                                </Text>
                              </>
                            ) : null}
                          </View>
                        </View>
                        <Ionicons
                          name={expanded ? "chevron-up" : "chevron-down"}
                          size={18}
                          color={colors.textTertiary}
                        />
                      </View>
                    </AnimatedPressable>

                    {expanded && (
                      <View style={styles.serverExpandedContent}>
                        {connectionIssue ? (
                          <ServerNoticeCard
                            icon="alert-circle-outline"
                            accent={connectionIssueAccent(
                              connectionIssue,
                              colors,
                            )}
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
                            accessibilityRole="button"
                            accessibilityLabel={`${actionLabel} ${server.name}`}
                            onPress={() => {
                              void (connectionState === "connected" && current
                                ? disconnectServer(server.id)
                                : connectServer(server));
                              void Haptics.impactAsync(
                                Haptics.ImpactFeedbackStyle.Light,
                              );
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
                            accessibilityRole="button"
                            accessibilityLabel={`Edit ${server.name}`}
                            onPress={() => {
                              openEditServer(server);
                              void Haptics.impactAsync(
                                Haptics.ImpactFeedbackStyle.Light,
                              );
                            }}
                          >
                            <Text style={styles.actionBtnText}>Edit</Text>
                          </AnimatedPressable>
                          <AnimatedPressable
                            style={[styles.actionBtn, styles.actionBtnDanger]}
                            preset="press"
                            scale={0.95}
                            accessibilityRole="button"
                            accessibilityLabel={`Remove ${server.name}`}
                            onPress={() => {
                              handleDeleteServer(server);
                              void Haptics.impactAsync(
                                Haptics.ImpactFeedbackStyle.Medium,
                              );
                            }}
                          >
                            <Text
                              style={[
                                styles.actionBtnText,
                                styles.actionBtnDangerText,
                              ]}
                            >
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
            <AnimatedPressable
              style={styles.addServerRow}
              preset="press"
              scale={0.99}
              accessibilityRole="button"
              accessibilityLabel="Add server"
              accessibilityHint="Pair a server using a link or QR code"
              onPress={() => {
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                openCreateServer();
              }}
            >
              <View style={styles.addServerIcon}>
                <Ionicons name="add" size={20} color={colors.textOnAccent} />
              </View>
              <View style={styles.addServerCopy}>
                <Text style={styles.addServerTitle}>Add Server</Text>
                <Text style={styles.addServerSubtitle}>
                  Pair via link or QR code
                </Text>
              </View>
              <Ionicons
                name="chevron-forward"
                size={18}
                color={colors.textTertiary}
              />
            </AnimatedPressable>
          </View>

          <View style={styles.sectionHeaderStandalone}>
            <Text style={styles.sectionLabel} accessibilityRole="header">
              Appearance
            </Text>
          </View>
          <View
            style={styles.appearanceGroup}
            accessibilityRole="radiogroup"
            accessibilityLabel="Appearance theme"
          >
            {THEME_CHOICES.map((choice, index) => {
              const selected = preference === choice.value;
              return (
                <AnimatedPressable
                  key={choice.value}
                  style={[
                    styles.appearanceRow,
                    index < THEME_CHOICES.length - 1 && styles.groupRowBorder,
                    selected && styles.appearanceRowSelected,
                  ]}
                  preset="press"
                  scale={0.99}
                  accessibilityRole="radio"
                  accessibilityLabel={`${choice.label} appearance`}
                  accessibilityState={{ checked: selected }}
                  aria-checked={selected}
                  onPress={() => {
                    void Haptics.selectionAsync();
                    void setPreference(choice.value);
                  }}
                >
                  <Ionicons
                    name={choice.icon}
                    size={20}
                    color={
                      selected ? colors.accentStrong : colors.textSecondary
                    }
                  />
                  <Text
                    style={[
                      styles.appearanceLabel,
                      selected && styles.appearanceLabelSelected,
                    ]}
                  >
                    {choice.label}
                  </Text>
                  <Ionicons
                    name={selected ? "radio-button-on" : "radio-button-off"}
                    size={20}
                    color={selected ? colors.accentStrong : colors.textTertiary}
                  />
                </AnimatedPressable>
              );
            })}
          </View>
          <Text style={styles.appearanceHint}>
            System follows this device. Light and Dark stay fixed until changed.
          </Text>

          <View style={styles.sectionHeaderStandalone}>
            <Text style={styles.sectionLabel} accessibilityRole="header">
              About
            </Text>
          </View>
          <View style={styles.aboutGroup}>
            <View style={styles.aboutRow}>
              <View style={styles.aboutCopy}>
                <Text style={styles.aboutTitle}>Zen</Text>
                <Text style={styles.aboutDescription}>
                  Mobile-native agent control plane
                </Text>
              </View>
              <Text style={styles.version}>Version {appVersion}</Text>
            </View>
          </View>
        </View>
      </ScrollView>

      {/* Unified Pair/Edit presentation: one RisingSheet Modal, editor|scanner modes */}
      <RisingSheet
        visible={pairPresentation.mode !== "closed"}
        onClose={handlePairPresentationDismiss}
        layout={pairPresentation.mode === "scanner" ? "fullscreen" : "card"}
        cardStyle={
          pairPresentation.mode === "scanner"
            ? styles.scannerSheetCard
            : styles.modalCard
        }
        avoidKeyboard={pairPresentation.mode === "editor"}
      >
        {pairPresentation.mode === "scanner" ? (
          <View
            style={[
              styles.scannerScreen,
              {
                paddingTop: Math.max(insets.top, 20),
                paddingBottom: Math.max(insets.bottom, 20),
              },
            ]}
            accessibilityViewIsModal
          >
            <View style={styles.scannerHeader}>
              <Text style={styles.scannerTitle} accessibilityRole="header">
                Scan Pairing QR
              </Text>
              <AnimatedPressable
                preset="press"
                scale={0.9}
                style={[
                  styles.scannerCloseButton,
                  pairPresentation.scannerLocked && styles.scannerBtnDisabled,
                ]}
                accessibilityRole="button"
                accessibilityLabel="Close QR scanner"
                accessibilityState={{
                  disabled: pairPresentation.scannerLocked,
                }}
                disabled={pairPresentation.scannerLocked}
                onPress={() => {
                  Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                  closeScanner();
                }}
              >
                <Ionicons
                  name="close"
                  size={24}
                  color={SCANNER_COLORS.textPrimary}
                />
              </AnimatedPressable>
            </View>

            {cameraMountError ? (
              <View style={styles.scannerNoticeCard}>
                <Text style={styles.scannerNoticeTitle}>
                  Camera unavailable
                </Text>
                <Text style={styles.scannerNoticeText}>{cameraMountError}</Text>
              </View>
            ) : !cameraPermission ? (
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
                  style={[
                    styles.scannerPrimaryBtn,
                    styles.scannerPermissionBtn,
                  ]}
                  preset="press"
                  scale={0.96}
                  accessibilityRole="button"
                  accessibilityLabel="Grant camera access"
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
                      pairPresentation.scannerLocked
                        ? undefined
                        : handleScanResult
                    }
                    onMountError={(event) => {
                      const message =
                        event?.message ||
                        "The camera could not start. Pick a QR image instead, or paste the pairing link.";
                      setCameraMountError(message);
                    }}
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
                style={[
                  styles.scannerSecondaryBtn,
                  pairPresentation.scannerLocked && styles.scannerBtnDisabled,
                ]}
                preset="press"
                scale={0.96}
                disabled={pairPresentation.scannerLocked}
                accessibilityRole="button"
                accessibilityLabel={
                  pairPresentation.scannerLocked
                    ? "Reading QR image"
                    : "Pick QR image"
                }
                accessibilityState={{
                  disabled: pairPresentation.scannerLocked,
                  busy: pairPresentation.scannerLocked,
                }}
                onPress={() => void handlePickScannerImage()}
              >
                <Text
                  style={[
                    styles.scannerSecondaryBtnText,
                    pairPresentation.scannerLocked &&
                      styles.scannerDisabledBtnText,
                  ]}
                >
                  {pairPresentation.scannerLocked
                    ? "Reading Image..."
                    : "Pick QR Image"}
                </Text>
              </AnimatedPressable>
              <AnimatedPressable
                style={[
                  styles.scannerPrimaryBtn,
                  pairPresentation.scannerLocked && styles.scannerBtnDisabled,
                ]}
                preset="press"
                scale={0.96}
                accessibilityRole="button"
                accessibilityLabel="Close QR scanner"
                accessibilityState={{
                  disabled: pairPresentation.scannerLocked,
                }}
                disabled={pairPresentation.scannerLocked}
                onPress={() => {
                  Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                  closeScanner();
                }}
              >
                <Text style={styles.scannerPrimaryBtnText}>Done</Text>
              </AnimatedPressable>
            </View>
          </View>
        ) : (
          <ScrollView
            style={styles.modalScroll}
            contentContainerStyle={styles.modalScrollContent}
            keyboardShouldPersistTaps="handled"
            showsVerticalScrollIndicator={false}
          >
            <Text style={styles.modalTitle} accessibilityRole="header">
              {editingServerId ? "Edit Server" : "Pair Server"}
            </Text>

            {editingServer ? (
              <>
                <Text style={styles.fieldLabel}>Name</Text>
                <TextInput
                  style={styles.input}
                  value={draftName}
                  onChangeText={setDraftName}
                  accessibilityLabel="Server name"
                  placeholder="workstation"
                  placeholderTextColor={colors.textSecondary}
                  autoCapitalize="none"
                  autoCorrect={false}
                />

                {editingServer.transportKind === "link" ? (
                  <View style={styles.identityCard}>
                    <Text style={styles.identityLabel}>Connection path</Text>
                    <Text style={styles.fieldHint}>
                      Zen Link chooses a pinned relay candidate automatically.
                      The saved daemon remains the same current server.
                    </Text>
                  </View>
                ) : (
                  <>
                    <Text style={[styles.fieldLabel, { marginTop: 16 }]}>
                      Advanced / Self-managed endpoint
                    </Text>
                    <TextInput
                      style={styles.input}
                      value={draftEndpoint}
                      onChangeText={setDraftEndpoint}
                      accessibilityLabel="Server endpoint"
                      placeholder="wss://zen.example.com/ws"
                      placeholderTextColor={colors.textSecondary}
                      autoCapitalize="none"
                      autoCorrect={false}
                    />
                    <Text style={styles.fieldHint}>
                      Full-origin endpoint from LAN, Tailscale, Cloudflare
                      Tunnel, or your reverse proxy.
                    </Text>
                  </>
                )}

                <View style={styles.identityCard}>
                  <Text style={styles.identityLabel}>Trusted Daemon</Text>
                  <Text style={styles.identityCode} numberOfLines={1}>
                    {editingServer.daemonId}
                  </Text>
                </View>

                <View style={styles.modalActions}>
                  <AnimatedPressable
                    style={styles.modalBtn}
                    preset="press"
                    scale={0.94}
                    accessibilityRole="button"
                    accessibilityLabel="Cancel"
                    onPress={closeEditor}
                  >
                    <Text style={styles.modalBtnText}>Cancel</Text>
                  </AnimatedPressable>
                  <AnimatedPressable
                    style={[styles.modalBtn, styles.modalBtnPrimary]}
                    preset="press"
                    scale={0.94}
                    accessibilityRole="button"
                    onPress={() => {
                      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                      void handleSaveServer();
                    }}
                  >
                    <Text
                      style={[styles.modalBtnText, styles.modalBtnPrimaryText]}
                    >
                      Save
                    </Text>
                  </AnimatedPressable>
                </View>
              </>
            ) : (
              <>
                <Text style={styles.importLead}>
                  Scan the one-time QR from zen pair, or paste its pairing link.
                </Text>

                <Text style={styles.fieldLabel}>Pairing Link</Text>
                <TextInput
                  style={[styles.input, styles.importInput]}
                  value={draftImportValue}
                  onChangeText={setDraftImportValue}
                  accessibilityLabel="Pairing link"
                  placeholder="zen://settings?p=..."
                  placeholderTextColor={colors.textSecondary}
                  autoCapitalize="none"
                  autoCorrect={false}
                  multiline
                  textAlignVertical="top"
                />
                <Text style={styles.fieldHint}>
                  Advanced / Self-managed: run zen pair &lt;endpoint&gt; and
                  import the same Pairing V1 QR. Existing LAN, Tailscale,
                  Cloudflare Tunnel, and reverse proxy paths remain supported.
                </Text>

                <View style={styles.modalActions}>
                  <AnimatedPressable
                    style={styles.modalBtn}
                    preset="press"
                    scale={0.94}
                    accessibilityRole="button"
                    accessibilityLabel="Cancel"
                    onPress={closeEditor}
                  >
                    <Text style={styles.modalBtnText}>Cancel</Text>
                  </AnimatedPressable>
                  <AnimatedPressable
                    style={[styles.modalBtn, styles.modalBtnPrimary]}
                    preset="press"
                    scale={0.94}
                    accessibilityRole="button"
                    onPress={() => {
                      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                      void handleImportDraft();
                    }}
                  >
                    <Text
                      style={[styles.modalBtnText, styles.modalBtnPrimaryText]}
                    >
                      Import
                    </Text>
                  </AnimatedPressable>
                </View>

                <AnimatedPressable
                  style={styles.scanQrBtn}
                  preset="press"
                  scale={0.98}
                  accessibilityRole="button"
                  accessibilityLabel="Scan QR code"
                  onPress={() => {
                    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                    openScanner();
                  }}
                >
                  <Ionicons
                    name="qr-code-outline"
                    size={18}
                    color={colors.accent}
                  />
                  <Text style={styles.scanQrBtnText}>Scan QR Code</Text>
                </AnimatedPressable>
              </>
            )}
          </ScrollView>
        )}
      </RisingSheet>
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

function connectionColor(
  state: ConnectionState,
  colors: typeof Colors = Colors,
): string {
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

function latencyColor(
  latencyMs: number,
  colors: typeof Colors = Colors,
): string {
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

  return StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: colors.bgPrimary,
    },
    scrollView: {
      flex: 1,
    },
    content: {
      width: "100%",
      paddingHorizontal: 16,
      paddingTop: 20,
    },
    contentInner: {
      width: "100%",
      maxWidth: 760,
      alignSelf: "center",
    },

    sectionHeader: {
      flexDirection: "row",
      flexWrap: "wrap",
      alignItems: "baseline",
      justifyContent: "space-between",
      gap: 8,
      marginBottom: 10,
    },
    sectionHeaderStandalone: {
      marginTop: 28,
      marginBottom: 10,
    },
    sectionLabel: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textSecondary,
    },

    serverList: {
      overflow: "hidden",
      borderRadius: Radii.sm,
      backgroundColor: colors.bgSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    serverCard: {
      backgroundColor: colors.bgSurface,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    serverHeaderButton: {
      minHeight: 72,
      paddingHorizontal: 14,
      paddingVertical: 12,
      backgroundColor: colors.bgSurface,
    },
    serverExpandedContent: {
      paddingHorizontal: 14,
      paddingBottom: 14,
      backgroundColor: colors.surfaceSubtle,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    serverRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
    },
    statusDot: {
      width: 8,
      height: 8,
      borderRadius: 4,
      flexShrink: 0,
    },
    serverInfo: {
      flex: 1,
      minWidth: 0,
    },
    serverStatus: {
      flexDirection: "row",
      flexWrap: "wrap",
      alignItems: "center",
      gap: 5,
      marginTop: 2,
    },
    serverName: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.textPrimary,
    },
    serverUrl: {
      ...UiTextMetrics,
      ...TypeScale.mono,
      color: colors.textSecondary,
    },
    connectionLabel: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textTertiary,
    },
    connectionLabelActive: {
      color: colors.statusRunning,
    },
    metadataSeparator: {
      ...TypeScale.caption,
      color: colors.textTertiary,
    },
    latencyLabel: {
      ...UiTextMetrics,
      ...TypeScale.caption,
    },
    noticeCard: {
      marginTop: 12,
      padding: 12,
      borderRadius: Radii.xs,
      borderLeftWidth: 3,
      backgroundColor: colors.bgSurface,
    },
    noticeHeader: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
    },
    noticeTitle: {
      ...UiTextMetrics,
      ...TypeScale.label,
      flex: 1,
      color: colors.textPrimary,
    },
    noticeDetail: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      marginTop: 7,
      color: colors.textSecondary,
    },
    noticeHint: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      marginTop: 7,
      color: colors.textTertiary,
    },
    serverActions: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 8,
      marginTop: 12,
      paddingTop: 12,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    actionBtn: {
      minHeight: 44,
      minWidth: 88,
      flexGrow: 1,
      paddingHorizontal: 14,
      borderRadius: Radii.xs,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfacePressed,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    actionBtnText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textPrimary,
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
      paddingVertical: 22,
      gap: 2,
    },
    emptyText: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textPrimary,
    },
    emptyHint: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textTertiary,
    },
    addServerRow: {
      minHeight: 60,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingHorizontal: 14,
      paddingVertical: 8,
      backgroundColor: colors.bgSurface,
    },
    addServerIcon: {
      width: 32,
      height: 32,
      borderRadius: 16,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.accent,
    },
    addServerCopy: {
      flex: 1,
      minWidth: 0,
    },
    addServerTitle: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textPrimary,
    },
    addServerSubtitle: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textTertiary,
    },

    appearanceGroup: {
      overflow: "hidden",
      borderRadius: Radii.sm,
      backgroundColor: colors.bgSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    appearanceRow: {
      minHeight: 52,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingHorizontal: 14,
      paddingVertical: 8,
      backgroundColor: colors.bgSurface,
    },
    appearanceRowSelected: {
      backgroundColor: colors.surfaceActive,
    },
    groupRowBorder: {
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    appearanceLabel: {
      ...UiTextMetrics,
      ...TypeScale.body,
      flex: 1,
      color: colors.textPrimary,
    },
    appearanceLabelSelected: {
      color: colors.accentStrong,
      fontFamily: Typography.uiFontMedium,
      fontWeight: "500",
    },
    appearanceHint: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      marginTop: 8,
      paddingHorizontal: 2,
      color: colors.textTertiary,
    },
    aboutGroup: {
      borderRadius: Radii.sm,
      backgroundColor: colors.bgSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    aboutRow: {
      minHeight: 64,
      flexDirection: "row",
      flexWrap: "wrap",
      alignItems: "center",
      gap: 12,
      paddingHorizontal: 14,
      paddingVertical: 10,
    },
    aboutCopy: {
      flex: 1,
      minWidth: 180,
    },
    aboutTitle: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.textPrimary,
    },
    aboutDescription: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textTertiary,
    },
    version: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textTertiary,
    },

    // Modal / editor
    modalCard: {
      borderRadius: Radii.md,
      padding: 20,
      maxWidth: 480,
      width: "100%",
      maxHeight: "92%",
      alignSelf: "center",
      backgroundColor: colors.modalSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
      ...shadow("float", colors.shadowColor),
    },
    scannerSheetCard: {
      flex: 1,
      width: "100%",
      backgroundColor: SCANNER_COLORS.bgPrimary,
    },
    modalScroll: {
      flexGrow: 0,
    },
    modalScrollContent: {
      paddingBottom: 2,
    },
    modalTitle: {
      ...UiTextMetrics,
      ...TypeScale.heading,
      color: colors.textPrimary,
      marginBottom: 18,
    },
    importLead: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textSecondary,
      marginBottom: 18,
    },
    fieldLabel: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textSecondary,
      marginBottom: 7,
    },
    input: {
      ...UiTextMetrics,
      ...TypeScale.mono,
      minHeight: 44,
      backgroundColor: colors.inputBackground,
      borderRadius: Radii.sm,
      paddingHorizontal: 14,
      paddingVertical: 10,
      color: colors.textPrimary,
      borderWidth: 1,
      borderColor: colors.borderStrong,
    },
    importInput: {
      minHeight: 112,
      paddingTop: 10,
    },
    fieldHint: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      marginTop: 8,
      color: colors.textTertiary,
    },
    identityCard: {
      marginTop: 16,
      borderRadius: Radii.sm,
      padding: 12,
      backgroundColor: colors.surfaceSubtle,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    identityLabel: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textSecondary,
      marginBottom: 6,
    },
    identityCode: {
      ...UiTextMetrics,
      ...TypeScale.mono,
      color: colors.textPrimary,
    },
    modalActions: {
      flexDirection: "row",
      flexWrap: "wrap",
      justifyContent: "flex-end",
      gap: 10,
      marginTop: 22,
    },
    modalBtn: {
      minWidth: 96,
      minHeight: 44,
      flexGrow: 1,
      borderRadius: Radii.sm,
      alignItems: "center",
      justifyContent: "center",
      paddingHorizontal: 16,
      backgroundColor: colors.surfacePressed,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    modalBtnPrimary: {
      backgroundColor: colors.accent,
      borderColor: colors.accent,
    },
    modalBtnText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textPrimary,
    },
    modalBtnPrimaryText: {
      color: colors.textOnAccent,
    },
    scanQrBtn: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 8,
      minHeight: 44,
      marginTop: 14,
      paddingHorizontal: 14,
      borderRadius: Radii.sm,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
      backgroundColor: colors.surfaceSubtle,
    },
    scanQrBtnText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.accentStrong,
    },

    scannerScreen: {
      flex: 1,
      backgroundColor: SCANNER_COLORS.bgPrimary,
      paddingHorizontal: 16,
    },
    scannerHeader: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      marginBottom: 14,
    },
    scannerCloseButton: {
      width: 44,
      height: 44,
      alignItems: "center",
      justifyContent: "center",
      borderRadius: Radii.pill,
      backgroundColor: SCANNER_COLORS.surfaceSubtle,
    },
    scannerTitle: {
      ...UiTextMetrics,
      ...TypeScale.title,
      flex: 1,
      color: SCANNER_COLORS.textPrimary,
    },
    scannerViewport: {
      flex: 1,
      minHeight: 240,
      borderRadius: Radii.md,
      overflow: "hidden",
      backgroundColor: SCANNER_COLORS.bgPrimary,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: SCANNER_COLORS.border,
    },
    scannerCamera: {
      flex: 1,
      minHeight: 240,
    },
    scannerOverlay: {
      ...StyleSheet.absoluteFill,
    },
    scannerMaskTop: {
      flex: 1,
      backgroundColor: "rgba(0,0,0,0.56)",
    },
    scannerMaskMiddle: {
      height: 220,
      flexDirection: "row",
    },
    scannerMaskSide: {
      flex: 1,
      backgroundColor: "rgba(0,0,0,0.56)",
    },
    scannerFrame: {
      width: 220,
      borderRadius: Radii.md,
      borderWidth: 1,
      borderColor: SCANNER_COLORS.borderStrong,
    },
    scannerMaskBottom: {
      flex: 1,
      backgroundColor: "rgba(0,0,0,0.56)",
    },
    scannerFrameCornerTopLeft: {
      position: "absolute",
      top: -1,
      left: -1,
      width: 30,
      height: 30,
      borderTopWidth: 4,
      borderLeftWidth: 4,
      borderColor: SCANNER_COLORS.focusRing,
      borderTopLeftRadius: Radii.md,
    },
    scannerFrameCornerTopRight: {
      position: "absolute",
      top: -1,
      right: -1,
      width: 30,
      height: 30,
      borderTopWidth: 4,
      borderRightWidth: 4,
      borderColor: SCANNER_COLORS.focusRing,
      borderTopRightRadius: Radii.md,
    },
    scannerFrameCornerBottomLeft: {
      position: "absolute",
      bottom: -1,
      left: -1,
      width: 30,
      height: 30,
      borderBottomWidth: 4,
      borderLeftWidth: 4,
      borderColor: SCANNER_COLORS.focusRing,
      borderBottomLeftRadius: Radii.md,
    },
    scannerFrameCornerBottomRight: {
      position: "absolute",
      bottom: -1,
      right: -1,
      width: 30,
      height: 30,
      borderBottomWidth: 4,
      borderRightWidth: 4,
      borderColor: SCANNER_COLORS.focusRing,
      borderBottomRightRadius: Radii.md,
    },
    scannerHelpText: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      marginTop: 12,
      color: SCANNER_COLORS.textSecondary,
      textAlign: "center",
    },
    scannerActions: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 10,
      marginTop: 12,
    },
    scannerNoticeCard: {
      marginTop: 18,
      borderRadius: Radii.md,
      padding: 20,
      backgroundColor: SCANNER_COLORS.surfaceSubtle,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: SCANNER_COLORS.border,
      alignItems: "center",
    },
    scannerNoticeTitle: {
      ...UiTextMetrics,
      ...TypeScale.heading,
      color: SCANNER_COLORS.textPrimary,
      textAlign: "center",
    },
    scannerNoticeText: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      marginTop: 8,
      color: SCANNER_COLORS.textSecondary,
      textAlign: "center",
    },
    scannerPrimaryBtn: {
      flexGrow: 1,
      flexBasis: 132,
      marginTop: 12,
      minHeight: 44,
      borderRadius: Radii.sm,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: SCANNER_COLORS.accent,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: SCANNER_COLORS.accent,
      paddingHorizontal: 16,
    },
    scannerPrimaryBtnText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: SCANNER_COLORS.textOnAccent,
      textAlign: "center",
    },
    scannerPermissionBtn: {
      width: "100%",
      maxWidth: 280,
      flexGrow: 0,
      flexBasis: "auto",
    },
    scannerSecondaryBtn: {
      flexGrow: 1,
      flexBasis: 132,
      marginTop: 12,
      minHeight: 44,
      borderRadius: Radii.sm,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: SCANNER_COLORS.surfacePressed,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: SCANNER_COLORS.borderStrong,
      paddingHorizontal: 16,
    },
    scannerBtnDisabled: {
      backgroundColor: SCANNER_COLORS.disabledSurface,
      borderColor: SCANNER_COLORS.border,
    },
    scannerSecondaryBtnText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: SCANNER_COLORS.textPrimary,
      textAlign: "center",
    },
    scannerDisabledBtnText: {
      color: SCANNER_COLORS.disabledText,
    },
  });
}
