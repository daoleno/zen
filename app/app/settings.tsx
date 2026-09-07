import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Alert,
  KeyboardAvoidingView,
  LayoutAnimation,
  Linking,
  Platform,
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
import type { TelegramConnectionStatus } from "../services/websocket";
import {
  ConnectionState,
  countWorkersByServer,
  useWorkerList,
  useWorkerServerSummary,
} from "../store/workers";
import * as Storage from "../services/storage";
import { connectionIssueAccent } from "../services/connectionIssue";
import { AnimatedPressable } from "../components/ui/AnimatedPressable";
import { RisingSheet } from "../components/ui/RisingSheet";
import { cancelCalendarNotifications } from "../services/calendarNotifications";
import { useCurrentServer } from "../store/currentServer";
import {
  telegramSetupMode,
} from "../components/settings/connectionPresentation";

const QR_BARCODE_TYPES: BarcodeType[] = ["qr"];
const SCANNER_COLORS = ZEN_DARK_APP_COLORS;
const TELEGRAM_BOTFATHER_URL = "https://t.me/BotFather";
const THEME_CHOICES = [
  { label: "System", value: "system", icon: "phone-portrait-outline" },
  { label: "Light", value: "classic-light", icon: "sunny-outline" },
  { label: "Dark", value: "classic-dark", icon: "moon-outline" },
] as const;

export default function SettingsScreen() {
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const agents = useWorkerList();
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
  } = useWorkerServerSummary();
  const agentCounts = useMemo(() => countWorkersByServer(agents), [agents]);
  const colors = useAppColors();
  const { preference, setPreference } = useZenTheme();
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);
  const params = useLocalSearchParams<{
    addServer?: string;
    refresh?: string;
    pairingRequired?: string;
    pairMode?: string;
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
    if (params.pairMode === "scanner") openScanner();
    setHandledAutoOpenToken(params.addServer);
  }, [handledAutoOpenToken, loaded, params.addServer, params.pairMode]);

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
        router.replace({ pathname: "/onboarding", params: { paired: "1" } });
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
              Servers
            </Text>
          </View>

          <View style={styles.serverList}>
            {servers.length === 0 ? (
              <View style={styles.emptyCard}>
                <Text style={styles.emptyText}>No paired servers</Text>
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
                const waitingForWorkers =
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
                              ? "Zen Link"
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

                        {waitingForWorkers ? (
                          <ServerNoticeCard
                            icon="information-circle-outline"
                            accent={colors.accent}
                            title={
                              hydrated
                                ? "No active agents"
                                : "Loading agents"
                            }
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
              style={styles.addConnectionRow}
              preset="press"
              scale={0.99}
              accessibilityRole="button"
              accessibilityLabel="Pair a server"
              onPress={() => {
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                openCreateServer();
              }}
            >
              <View style={styles.addConnectionIcon}>
                <Ionicons name="add" size={20} color={colors.textOnAccent} />
              </View>
              <View style={styles.addConnectionCopy}>
                <Text style={styles.addConnectionTitle}>Pair a server</Text>
              </View>
              <Ionicons
                name="chevron-forward"
                size={18}
                color={colors.textTertiary}
              />
            </AnimatedPressable>
          </View>

          <View style={styles.sectionHeaderStandalone}>
            <Text style={styles.sectionLabel} accessibilityRole="header">Channels</Text>
          </View>
          {currentServerId ? (
            <TelegramConnectionRow
              key={currentServerId || "no-current-server"}
              serverId={currentServerId}
              connected={serverConnections[currentServerId] === "connected"}
            />
          ) : <Text style={styles.emptyText}>No current server</Text>}

          <View style={styles.sectionHeaderStandalone}>
            <Text style={styles.sectionLabel} accessibilityRole="header">
              Providers
            </Text>
          </View>
          <View style={styles.aboutGroup}>
            <AnimatedPressable
              style={styles.aboutRow}
              preset="press"
              scale={0.99}
              accessibilityRole="button"
              accessibilityLabel="Providers"
              accessibilityHint="Manage Provider connections and API keys"
              onPress={() => {
                void Haptics.selectionAsync();
                router.push("/model-profiles");
              }}
            >
              <View style={styles.aboutCopy}>
                <Text style={styles.aboutTitle}>Models and accounts</Text>
                {!currentServerId ? <Text style={styles.aboutDescription}>No current server</Text> : null}
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

          <View style={styles.sectionHeaderStandalone}>
            <Text style={styles.sectionLabel} accessibilityRole="header">
              About
            </Text>
          </View>
          <View style={styles.aboutGroup}>
            <View style={styles.aboutRow}>
              <View style={styles.aboutCopy}>
                <Text style={styles.aboutTitle}>Zen</Text>
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
                  selectionColor={colors.selectionBackground}
                  cursorColor={colors.accentStrong}
                  autoCapitalize="none"
                  autoCorrect={false}
                />

                {editingServer.transportKind === "link" ? (
                  <View style={styles.identityCard}>
                    <Text style={styles.identityLabel}>Connection path</Text>
                    <Text style={styles.fieldHint}>Zen Link</Text>
                  </View>
                ) : (
                  <>
                    <Text style={[styles.fieldLabel, { marginTop: 16 }]}>
                      Server endpoint
                    </Text>
                    <TextInput
                      style={styles.input}
                      value={draftEndpoint}
                      onChangeText={setDraftEndpoint}
                      accessibilityLabel="Server endpoint"
                      placeholder="wss://zen.example.com/ws"
                      placeholderTextColor={colors.textSecondary}
                  selectionColor={colors.selectionBackground}
                  cursorColor={colors.accentStrong}
                      autoCapitalize="none"
                      autoCorrect={false}
                    />
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
                  selectionColor={colors.selectionBackground}
                  cursorColor={colors.accentStrong}
                  autoCapitalize="none"
                  autoCorrect={false}
                  multiline
                  textAlignVertical="top"
                />
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

function TelegramConnectionRow({
  serverId,
  connected,
}: {
  serverId: string | null;
  connected: boolean;
}) {
  const { theme } = useAppTheme();
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(theme), [theme]);
  const [status, setStatus] = useState<TelegramConnectionStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [showToken, setShowToken] = useState(false);
  const [token, setToken] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const ownerActive = useRef(false);
  const ownerEpoch = useRef(0);
  const setupMode = telegramSetupMode(serverId || undefined, connected);
  const activeServerId = setupMode === "direct" && serverId ? serverId : null;
  const visibleStatus = activeServerId ? status : null;

  useFocusEffect(
    useCallback(() => {
      let cancelled = false;
      ownerEpoch.current++;
      ownerActive.current = connected && Boolean(serverId);
      setBusy(false);
      setLoadError(null);
      if (!serverId || !connected) {
        setStatus(null);
        setLoading(false);
        return () => {
          cancelled = true;
          ownerActive.current = false;
          ownerEpoch.current++;
          setToken("");
          setShowToken(false);
        };
      }
      setLoading(true);
      void wsClient
        .getTelegramConnectionStatus(serverId)
        .then((next) => {
          if (!cancelled) setStatus(next);
        })
        .catch(() => {
          if (!cancelled) {
            setStatus(null);
            setLoadError("Telegram status could not be loaded.");
          }
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });
      return () => {
        cancelled = true;
        ownerActive.current = false;
        ownerEpoch.current++;
        setToken("");
        setShowToken(false);
      };
    }, [connected, serverId, reload]),
  );

  const runStatusMutation = async (
    operation: () => Promise<TelegramConnectionStatus>,
  ) => {
    if (!ownerActive.current) return;
    const epoch = ownerEpoch.current;
    setBusy(true);
    try {
      const next = await operation();
      if (epoch !== ownerEpoch.current) return;
      setStatus(next);
    } catch (error: any) {
      if (epoch !== ownerEpoch.current) return;
      Alert.alert(
        "Telegram connection",
        error?.message || "The connection could not be updated.",
      );
    } finally {
      if (epoch === ownerEpoch.current) setBusy(false);
    }
  };

  const configure = async () => {
    if (!ownerActive.current) return;
    const epoch = ownerEpoch.current;
    const credential = token.trim();
    if (!serverId || !credential) {
      setToken("");
      Alert.alert("Bot token required", "Enter the token issued by BotFather.");
      return;
    }
    setToken("");
    setBusy(true);
    try {
      const next = await wsClient.configureTelegramConnection(
        serverId,
        credential,
      );
      if (epoch !== ownerEpoch.current) return;
      setStatus(next);
      setShowToken(false);
    } catch (error: any) {
      if (epoch !== ownerEpoch.current) return;
      setToken("");
      Alert.alert(
        "Telegram setup failed",
        error?.message || "The bot token could not be verified.",
      );
    } finally {
      // The credential must not survive submission in component state.
      if (epoch === ownerEpoch.current) {
        setToken("");
        setBusy(false);
      }
    }
  };

  const openBotFather = async () => {
    try {
      await Linking.openURL(TELEGRAM_BOTFATHER_URL);
    } catch (error: any) {
      setToken("");
      Alert.alert(
        "BotFather unavailable",
        error?.message || "The official BotFather chat could not be opened.",
      );
    }
  };

  const pasteToken = async () => {
    try {
      setToken((await Clipboard.getStringAsync()).trim());
    } catch (error: any) {
      setToken("");
      Alert.alert(
        "Clipboard unavailable",
        error?.message || "The bot token could not be pasted.",
      );
    }
  };

  const beginBinding = async () => {
    if (!serverId || !ownerActive.current) return;
    const epoch = ownerEpoch.current;
    setBusy(true);
    try {
      const challenge = await wsClient.beginTelegramBinding(serverId);
      if (epoch !== ownerEpoch.current) return;
      await Linking.openURL(challenge.url);
      if (epoch !== ownerEpoch.current) return;
      const next = await wsClient.getTelegramConnectionStatus(serverId);
      if (epoch !== ownerEpoch.current) return;
      setStatus(next);
    } catch (error: any) {
      if (epoch !== ownerEpoch.current) return;
      setToken("");
      Alert.alert(
        "Telegram owner binding",
        error?.message || "The private Telegram chat could not be opened.",
      );
    } finally {
      if (epoch === ownerEpoch.current) setBusy(false);
    }
  };

  const confirmMutation = (
    title: string,
    message: string,
    actionLabel: string,
    operation: () => Promise<TelegramConnectionStatus>,
  ) => {
    Alert.alert(title, message, [
      { text: "Cancel", style: "cancel" },
      {
        text: actionLabel,
        style: "destructive",
        onPress: () => void runStatusMutation(operation),
      },
    ]);
  };

  const renderLocalTelegramSetup = () => (
    <View style={styles.telegramSetup}>
      <View style={styles.telegramSetupRow}>
        <View style={styles.telegramStepMarker}>
          <Text style={styles.telegramStepNumber}>1</Text>
        </View>
        <View style={styles.telegramStepContent}>
          <Text style={styles.telegramStepTitle}>Create or select a bot</Text>
          <View style={styles.telegramActions}>
            <ConnectionAction
              icon="open-outline"
              label="Open BotFather"
              accessibilityLabel="Open official BotFather chat in Telegram"
              onPress={() => void openBotFather()}
            />
          </View>
        </View>
      </View>
      <View
        style={[styles.telegramSetupRow, styles.telegramSetupRowLast]}
      >
        <View style={styles.telegramStepMarker}>
          <Text style={styles.telegramStepNumber}>2</Text>
        </View>
        <View style={styles.telegramStepContent}>
          <Text style={styles.telegramStepTitle}>On the machine running Zen</Text>
          <Text
            style={styles.telegramLocalCommand}
            selectable
            accessibilityLabel="Run zen telegram setup on the machine running Zen"
          >
            zen telegram setup
          </Text>
        </View>
      </View>
    </View>
  );

  const stateLabel = visibleStatus
    ? telegramConnectionStateLabel(visibleStatus.state)
    : loading
      ? "Loading"
      : setupMode === "local"
        ? "Local setup"
        : "Unavailable";
  const stateColor = visibleStatus
    ? telegramConnectionStateColor(visibleStatus.state, colors)
    : colors.textTertiary;
  const hasConfiguredBot = Boolean(visibleStatus?.bot_username);
  const hasBoundOwner = Boolean(visibleStatus?.owner_hint);
  const closeDetails = () => {
    setExpanded(false);
    setToken("");
    setShowToken(false);
  };
  return (
    <View style={styles.serverList}>
      <AnimatedPressable
        style={styles.telegramHeaderButton}
        preset="card"
        scale={0.99}
        accessibilityRole="button"
        accessibilityLabel={`Telegram${
          visibleStatus?.bot_username ? `, @${visibleStatus.bot_username}` : ""
        }, ${stateLabel}`}
        accessibilityHint="Open Telegram details and actions"
        accessibilityState={{ expanded }}
        onPress={() => {
          void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
          setExpanded((value) => {
            const next = !value;
            if (!next) {
              setToken("");
              setShowToken(false);
            }
            return next;
          });
        }}
      >
        <View style={styles.telegramHeader}>
          <View style={styles.telegramIcon}>
            <Ionicons name="paper-plane" size={18} color={colors.textOnAccent} />
          </View>
          <View style={styles.telegramHeadingCopy}>
            <Text style={styles.telegramTitle}>Telegram</Text>
            {visibleStatus?.bot_username ? (
              <Text style={styles.telegramIdentity} numberOfLines={1}>
                @{visibleStatus.bot_username}
              </Text>
            ) : null}
            <View
              style={styles.telegramState}
              accessibilityLabel={`Telegram ${stateLabel}`}
            >
              <View
                style={[styles.telegramStateDot, { backgroundColor: stateColor }]}
              />
              <Text style={[styles.telegramStateText, { color: stateColor }]}>
                {stateLabel}
              </Text>
            </View>
          </View>
          <Ionicons
            name="chevron-forward"
            size={18}
            color={colors.textTertiary}
          />
        </View>
      </AnimatedPressable>

      <RisingSheet visible={expanded} onClose={closeDetails} layout="fullscreen" cardStyle={{ backgroundColor: colors.bgPrimary }}>
        <SafeAreaView style={{ flex: 1 }}>
          <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === "ios" ? "padding" : "height"}>
            <View style={styles.telegramHeader}>
              <AnimatedPressable onPress={closeDetails} accessibilityRole="button" accessibilityLabel="Back to Settings" style={{ width: 48, height: 48, alignItems: "center", justifyContent: "center" }}>
                <Ionicons name="arrow-back" size={24} color={colors.textPrimary} />
              </AnimatedPressable>
              <Text style={styles.telegramTitle} accessibilityRole="header">Telegram</Text>
            </View>
        <ScrollView contentContainerStyle={styles.telegramExpandedContent} keyboardShouldPersistTaps="handled">
          {!loading && !loadError ? <Text style={styles.telegramDetail}>{stateLabel}</Text> : null}
          {!activeServerId ? (
            renderLocalTelegramSetup()
          ) : loading ? <Text style={styles.telegramDetail}>Loading</Text> : loadError ? (
            <View style={styles.telegramSetup}>
              <Text style={styles.telegramErrorText}>{loadError}</Text>
              <ConnectionAction icon="refresh" label="Retry" onPress={() => setReload(value => value + 1)} />
            </View>
          ) : (
            <>
              {status?.last_error ? (
                <View style={styles.telegramError}>
                  <Ionicons
                    name="alert-circle-outline"
                    size={16}
                    color={colors.dangerText}
                  />
                  <Text style={styles.telegramErrorText}>
                    {status.last_error}
                  </Text>
                </View>
              ) : null}

              {hasBoundOwner && status?.bot_name ? (
                <Text style={styles.telegramDetail}>
                  {status.bot_name} / {status.owner_hint}
                </Text>
              ) : null}

              {hasBoundOwner &&
              status &&
              (status.last_receive_at || status.last_send_at) ? (
                <Text style={styles.telegramMetadata}>
                  {status.last_receive_at
                    ? `Received ${formatConnectionTime(status.last_receive_at)}`
                    : "No messages received"}
                  {status.last_send_at
                    ? ` / Sent ${formatConnectionTime(status.last_send_at)}`
                    : ""}
                </Text>
              ) : null}

              {!hasBoundOwner && !showToken ? (
                <View style={styles.telegramSetup}>
                  <View style={styles.telegramSetupRow}>
                    <View
                      style={[
                        styles.telegramStepMarker,
                        hasConfiguredBot && styles.telegramStepMarkerComplete,
                      ]}
                    >
                      {hasConfiguredBot ? (
                        <Ionicons
                          name="checkmark"
                          size={14}
                          color={colors.textOnAccent}
                        />
                      ) : (
                        <Text style={styles.telegramStepNumber}>1</Text>
                      )}
                    </View>
                    <View style={styles.telegramStepContent}>
                      <Text style={styles.telegramStepTitle}>
                        Create or select a bot
                      </Text>
                      {!hasConfiguredBot ? (
                        <View style={styles.telegramActions}>
                          <ConnectionAction
                            icon="open-outline"
                            label="Open BotFather"
                            accessibilityLabel="Open official BotFather chat in Telegram"
                            disabled={busy}
                            onPress={() => void openBotFather()}
                          />
                        </View>
                      ) : null}
                    </View>
                  </View>

                  <View style={styles.telegramSetupRow}>
                    <View
                      style={[
                        styles.telegramStepMarker,
                        hasConfiguredBot && styles.telegramStepMarkerComplete,
                      ]}
                    >
                      {hasConfiguredBot ? (
                        <Ionicons
                          name="checkmark"
                          size={14}
                          color={colors.textOnAccent}
                        />
                      ) : (
                        <Text style={styles.telegramStepNumber}>2</Text>
                      )}
                    </View>
                    <View style={styles.telegramStepContent}>
                      <Text style={styles.telegramStepTitle}>
                        {hasConfiguredBot
                          ? `Bot verified @${status?.bot_username}`
                          : "Verify the bot token"}
                      </Text>
                      {!hasConfiguredBot ? (
                        <>
                          <View style={styles.telegramTokenInputRow}>
                            <TextInput
                              style={[styles.input, styles.telegramTokenInput]}
                              value={token}
                              onChangeText={setToken}
                              placeholder="BotFather token"
                              placeholderTextColor={colors.textSecondary}
                              selectionColor={colors.selectionBackground}
                              cursorColor={colors.accentStrong}
                              accessibilityLabel="Telegram bot token"
                              secureTextEntry
                              autoCapitalize="none"
                              autoCorrect={false}
                              editable={!busy}
                            />
                            <AnimatedPressable
                              style={[
                                styles.telegramPasteButton,
                                busy && styles.connectionActionDisabled,
                              ]}
                              preset="press"
                              scale={0.95}
                              accessibilityRole="button"
                              accessibilityLabel="Paste Telegram bot token from clipboard"
                              accessibilityState={{ disabled: busy }}
                              disabled={busy}
                              onPress={() => void pasteToken()}
                            >
                              <Ionicons
                                name="clipboard-outline"
                                size={16}
                                color={colors.textPrimary}
                              />
                              <Text style={styles.telegramPasteButtonText}>
                                Paste
                              </Text>
                            </AnimatedPressable>
                          </View>
                          <View style={styles.telegramActions}>
                            <ConnectionAction
                              icon="close"
                              label="Cancel"
                              disabled={busy}
                              onPress={() => {
                                setToken("");
                                setExpanded(false);
                              }}
                            />
                            <ConnectionAction
                              icon="checkmark"
                              label="Continue"
                              primary
                              disabled={busy || token.trim() === ""}
                              onPress={() => void configure()}
                            />
                          </View>
                        </>
                      ) : null}
                    </View>
                  </View>

                  <View
                    style={[
                      styles.telegramSetupRow,
                      styles.telegramSetupRowLast,
                    ]}
                  >
                    <View style={styles.telegramStepMarker}>
                      <Text style={styles.telegramStepNumber}>3</Text>
                    </View>
                    <View style={styles.telegramStepContent}>
                      <Text style={styles.telegramStepTitle}>
                        Connect your bot
                      </Text>
                      {hasConfiguredBot ? (
                        <View style={styles.telegramActions}>
                          <ConnectionAction
                            icon="open-outline"
                            label="Connect Telegram"
                            accessibilityLabel="Connect Telegram using the one-time binding link"
                            primary
                            disabled={busy}
                            onPress={() => void beginBinding()}
                          />
                          <ConnectionAction
                            icon="key-outline"
                            label="Rotate"
                            disabled={busy}
                            onPress={() => setShowToken(true)}
                          />
                          <ConnectionAction
                            icon="trash-outline"
                            label="Remove"
                            danger
                            disabled={busy}
                            onPress={() =>
                              confirmMutation(
                                "Remove Telegram bot",
                                "Delete the daemon token, owner binding, offsets, and delivery state? Telegram cloud messages are not deleted.",
                                "Remove",
                                () => wsClient.removeTelegramConnection(activeServerId),
                              )
                            }
                          />
                        </View>
                      ) : null}
                    </View>
                  </View>
                </View>
              ) : showToken ? (
                <View style={styles.telegramTokenForm}>
                  <View style={styles.telegramTokenInputRow}>
                    <TextInput
                      style={[styles.input, styles.telegramTokenInput]}
                      value={token}
                      onChangeText={setToken}
                      placeholder="BotFather token"
                      placeholderTextColor={colors.textSecondary}
                      selectionColor={colors.selectionBackground}
                      cursorColor={colors.accentStrong}
                      accessibilityLabel="Telegram bot token"
                      secureTextEntry
                      autoCapitalize="none"
                      autoCorrect={false}
                      editable={!busy}
                    />
                    <AnimatedPressable
                      style={[
                        styles.telegramPasteButton,
                        busy && styles.connectionActionDisabled,
                      ]}
                      preset="press"
                      scale={0.95}
                      accessibilityRole="button"
                      accessibilityLabel="Paste Telegram bot token from clipboard"
                      accessibilityState={{ disabled: busy }}
                      disabled={busy}
                      onPress={() => void pasteToken()}
                    >
                      <Ionicons
                        name="clipboard-outline"
                        size={16}
                        color={colors.textPrimary}
                      />
                      <Text style={styles.telegramPasteButtonText}>Paste</Text>
                    </AnimatedPressable>
                  </View>
                  <View style={styles.telegramActions}>
                    <ConnectionAction
                      icon="close"
                      label="Cancel"
                      disabled={busy}
                      onPress={() => {
                        setToken("");
                        setShowToken(false);
                        if (!hasConfiguredBot) {
                          setExpanded(false);
                        }
                      }}
                    />
                    <ConnectionAction
                      icon="checkmark"
                      label="Rotate"
                      primary
                      disabled={busy || token.trim() === ""}
                      onPress={() => void configure()}
                    />
                  </View>
                </View>
              ) : hasBoundOwner ? (
                <View style={styles.telegramActions}>
                  {status?.state === "disabled" && hasConfiguredBot ? (
                    <ConnectionAction
                      icon="power"
                      label="Enable"
                      primary
                      disabled={busy}
                      onPress={() =>
                        void runStatusMutation(() =>
                          wsClient.enableTelegramConnection(activeServerId),
                        )
                      }
                    />
                  ) : null}
                  {status?.enabled ? (
                    <ConnectionAction
                      icon="pause"
                      label="Disable"
                      disabled={busy}
                      onPress={() =>
                        confirmMutation(
                          "Disable Telegram",
                          "Stop receiving and sending Telegram messages for this daemon?",
                          "Disable",
                          () => wsClient.disableTelegramConnection(activeServerId),
                        )
                      }
                    />
                  ) : null}
                  {hasConfiguredBot ? (
                    <ConnectionAction
                      icon="key-outline"
                      label="Rotate"
                      disabled={busy}
                      onPress={() => setShowToken(true)}
                    />
                  ) : null}
                  {status?.owner_hint ? (
                    <ConnectionAction
                      icon="person-remove-outline"
                      label="Revoke"
                      danger
                      disabled={busy}
                      onPress={() =>
                        confirmMutation(
                          "Revoke Telegram owner",
                          "Remove the verified Telegram owner and require a new binding?",
                          "Revoke",
                          () => wsClient.revokeTelegramOwner(activeServerId),
                        )
                      }
                    />
                  ) : null}
                  {hasConfiguredBot ? (
                    <ConnectionAction
                      icon="trash-outline"
                      label="Remove"
                      danger
                      disabled={busy}
                      onPress={() =>
                        confirmMutation(
                          "Remove Telegram bot",
                          "Delete the daemon token, owner binding, offsets, and delivery state? Telegram cloud messages are not deleted.",
                          "Remove",
                          () => wsClient.removeTelegramConnection(activeServerId),
                        )
                      }
                    />
                  ) : null}
                </View>
              ) : null}
            </>
          )}
        </ScrollView>
          </KeyboardAvoidingView>
        </SafeAreaView>
      </RisingSheet>
    </View>
  );
}

function ConnectionAction({
  icon,
  label,
  accessibilityLabel = label,
  primary = false,
  danger = false,
  disabled = false,
  onPress,
}: {
  icon: React.ComponentProps<typeof Ionicons>["name"];
  label: string;
  accessibilityLabel?: string;
  primary?: boolean;
  danger?: boolean;
  disabled?: boolean;
  onPress(): void;
}) {
  const { theme } = useAppTheme();
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(theme), [theme]);
  const foreground = primary
    ? colors.textOnAccent
    : danger
      ? colors.dangerText
      : colors.textPrimary;
  return (
    <AnimatedPressable
      style={[
        styles.connectionAction,
        primary && styles.connectionActionPrimary,
        danger && styles.connectionActionDanger,
        disabled && styles.connectionActionDisabled,
      ]}
      preset="press"
      scale={0.95}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      accessibilityState={{ disabled, busy: disabled }}
      disabled={disabled}
      onPress={onPress}
    >
      <Ionicons name={icon} size={16} color={foreground} />
      <Text style={[styles.connectionActionText, { color: foreground }]}>
        {label}
      </Text>
    </AnimatedPressable>
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
  detail?: string;
  hint?: string;
}) {
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);

  return (
    <View style={[styles.noticeCard, { borderColor: accent }]}>
      <View style={styles.noticeHeader}>
        <Ionicons name={icon} size={15} color={accent} />
        <Text style={styles.noticeTitle}>{title}</Text>
      </View>
      {detail ? <Text style={styles.noticeDetail}>{detail}</Text> : null}
      {hint ? <Text style={styles.noticeHint}>{hint}</Text> : null}
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

function telegramConnectionStateLabel(
  state: TelegramConnectionStatus["state"],
): string {
  switch (state) {
    case "connected":
      return "Connected";
    case "setup_pending":
      return "Setup pending";
    case "degraded":
      return "Degraded";
    case "disabled":
      return "Disabled";
  }
}

function telegramConnectionStateColor(
  state: TelegramConnectionStatus["state"],
  colors: typeof Colors = Colors,
): string {
  switch (state) {
    case "connected":
      return colors.statusRunning;
    case "setup_pending":
      return colors.warning;
    case "degraded":
      return colors.dangerText;
    case "disabled":
      return colors.disabledText;
  }
}

function formatConnectionTime(value: string): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return "recently";
  return parsed.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
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
    telegramHeaderButton: {
      minHeight: 72,
      paddingHorizontal: 14,
      paddingVertical: 12,
      backgroundColor: colors.bgSurface,
    },
    telegramHeader: {
      minHeight: 44,
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
    },
    telegramIcon: {
      width: 36,
      height: 36,
      borderRadius: Radii.xs,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.accentStrong,
    },
    telegramHeadingCopy: {
      flex: 1,
      minWidth: 0,
    },
    telegramTitle: {
      ...UiTextMetrics,
      ...TypeScale.body,
      color: colors.textPrimary,
    },
    telegramIdentity: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textSecondary,
    },
    telegramState: {
      flexDirection: "row",
      alignItems: "center",
      alignSelf: "flex-start",
      marginTop: 3,
      gap: 5,
    },
    telegramStateDot: {
      width: 6,
      height: 6,
      borderRadius: 3,
      flexShrink: 0,
    },
    telegramStateText: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      flexShrink: 1,
    },
    telegramExpandedContent: {
      paddingHorizontal: 14,
      paddingBottom: 14,
      backgroundColor: colors.surfaceSubtle,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    telegramSetup: {
      marginTop: 4,
    },
    telegramSetupRow: {
      flexDirection: "row",
      gap: 10,
      paddingVertical: 12,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    telegramSetupRowLast: {
      borderBottomWidth: 0,
      paddingBottom: 0,
    },
    telegramStepMarker: {
      width: 24,
      height: 24,
      borderRadius: 12,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfacePressed,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderStrong,
    },
    telegramStepMarkerComplete: {
      backgroundColor: colors.accentStrong,
      borderColor: colors.accentStrong,
    },
    telegramStepNumber: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textSecondary,
    },
    telegramStepContent: {
      flex: 1,
      minWidth: 0,
    },
    telegramStepTitle: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      minHeight: 24,
      color: colors.textPrimary,
    },
    telegramDetail: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      marginTop: 12,
      color: colors.textSecondary,
    },
    telegramMetadata: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      marginTop: 8,
      color: colors.textTertiary,
    },
    telegramLocalCommand: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      marginTop: 8,
      color: colors.accentStrong,
    },
    telegramError: {
      marginTop: 12,
      padding: 10,
      flexDirection: "row",
      alignItems: "flex-start",
      gap: 8,
      borderRadius: Radii.xs,
      backgroundColor: colors.surfaceSubtle,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.dangerText,
    },
    telegramErrorText: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      flex: 1,
      color: colors.dangerText,
    },
    telegramTokenForm: {
      marginTop: 12,
      paddingTop: 12,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    telegramTokenInputRow: {
      flexDirection: "row",
      alignItems: "stretch",
      gap: 8,
      marginTop: 10,
    },
    telegramTokenInput: {
      flex: 1,
      minWidth: 0,
    },
    telegramPasteButton: {
      minHeight: 44,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 6,
      paddingHorizontal: 12,
      borderRadius: Radii.xs,
      backgroundColor: colors.surfacePressed,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    telegramPasteButtonText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textPrimary,
    },
    telegramActions: {
      marginTop: 12,
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 8,
    },
    connectionAction: {
      minHeight: 44,
      minWidth: 104,
      paddingHorizontal: 12,
      flexGrow: 1,
      flexBasis: 104,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 7,
      borderRadius: Radii.xs,
      backgroundColor: colors.surfacePressed,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    connectionActionPrimary: {
      backgroundColor: colors.accentStrong,
      borderColor: colors.accentStrong,
    },
    connectionActionDanger: {
      borderColor: colors.dangerText,
    },
    connectionActionDisabled: {
      opacity: 0.5,
    },
    connectionActionText: {
      ...UiTextMetrics,
      ...TypeScale.label,
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
    },
    emptyText: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textPrimary,
    },
    addConnectionRow: {
      minHeight: 60,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingHorizontal: 14,
      paddingVertical: 8,
      backgroundColor: colors.bgSurface,
    },
    addConnectionIcon: {
      width: 32,
      height: 32,
      borderRadius: 16,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.accent,
    },
    addConnectionCopy: {
      flex: 1,
      minWidth: 0,
    },
    addConnectionTitle: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textPrimary,
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
