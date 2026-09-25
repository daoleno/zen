import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Alert,
  AppState,
  KeyboardAvoidingView,
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
  ContinuousCorners,
  Radii,
  TypeScale,
  UiTextMetrics,
  useAppColors,
  useAppTheme,
  shadow,
  type AppColors,
} from "../constants/tokens";
import { useZenTheme, type ResolvedZenTheme } from "../theme";
import { ZEN_DARK_APP_COLORS } from "../theme/primitives";
import { appVersion } from "../constants/appVersion";
import { importConnection } from "../services/importConnection";
import { PairingCancelledError } from "../services/pairingScope";
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
import {
  ActionMenu,
  AppText,
  Button,
  InlineNotice,
  ListRow,
  ListSection,
  confirmDestructive,
} from "../components/ui";
import type { ActionMenuItem } from "../components/ui/ActionMenu";
import { ZenLogoMark } from "../components/ui/ZenLogoMark";
import { RisingSheet } from "../components/ui/RisingSheet";
import { TelegramConnectionPanel } from "../components/settings/TelegramConnectionPanel";
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
  const [serverMenuId, setServerMenuId] = useState<string | null>(null);
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
      if (error instanceof PairingCancelledError) {
        setPairPresentation((current) => returnToPairEditor(current));
        return false;
      }
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
    confirmDestructive({
      title: `Remove ${server.name}?`,
      message: "Its Sessions stay on the computer. Pair again to reconnect.",
      confirmLabel: "Remove",
      onConfirm: async () => {
        wsClient.disconnectServer(server.id);
        dispatch({ type: "REMOVE_SERVER", serverId: server.id });
        await Storage.removeServer(server.id);
        await cancelCalendarNotifications(server.id);
        await refreshServers();
      },
    });
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

  const currentIssue = currentServerId
    ? serverConnectionIssues[currentServerId] ?? null
    : null;
  const menuServer = servers.find((server) => server.id === serverMenuId) ?? null;
  const serverMenuItems = (server: Storage.StoredServer): ActionMenuItem[] => {
    const current = server.id === currentServerId;
    const connectionState = serverConnections[server.id] || "offline";
    const connectionIssue = serverConnectionIssues[server.id] || null;
    const agentCount = agentCounts[server.id] || 0;
    const hydrated = Boolean(hydratedServers[server.id]);
    const actionLabel = !current
      ? "Use"
      : connectionState === "connected"
        ? "Disconnect"
        : connectionState === "connecting" || connectionIssue
          ? "Retry"
          : "Connect";
    const primaryDetail =
      connectionIssue?.hint ??
      connectionIssue?.detail ??
      (current && connectionState === "connected"
        ? hydrated
          ? `${agentCount} ${agentCount === 1 ? "active agent" : "active agents"}`
          : "Loading agents"
        : undefined);
    return [
      {
        key: "connection",
        label: actionLabel,
        accessibilityLabel: `${actionLabel} ${server.name}`,
        detail: primaryDetail,
        icon: !current
          ? "swap-horizontal-outline"
          : connectionState === "connected"
            ? "power-outline"
            : "refresh-outline",
        onPress: () => {
          void (connectionState === "connected" && current
            ? disconnectServer(server.id)
            : connectServer(server));
        },
      },
      {
        key: "edit",
        label: "Edit",
        accessibilityLabel: `Edit ${server.name}`,
        icon: "create-outline",
        onPress: () => openEditServer(server),
      },
      {
        key: "remove",
        label: "Remove",
        accessibilityLabel: `Remove ${server.name}`,
        icon: "trash-outline",
        destructive: true,
        onPress: () => handleDeleteServer(server),
      },
    ];
  };

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
          <SettingsSectionHeader>Servers</SettingsSectionHeader>
          <ListSection
            footer={servers.length === 0 ? "Pair this phone with a computer running zen." : null}
          >
            {servers.map((server) => {
              const current = server.id === currentServerId;
              const connectionState = serverConnections[server.id] || "offline";
              const latencySample = serverLatencyById[server.id];
              const connectionIssue = serverConnectionIssues[server.id] || null;
              const endpoint =
                server.transportKind === "link" ? "Zen Link" : server.url;
              const status = [
                connectionIssue?.title ?? connectionLabel(connectionState),
                connectionState === "connected" && latencySample
                  ? formatLatency(latencySample.latencyMs)
                  : null,
              ]
                .filter(Boolean)
                .join(" · ");
              return (
                <ListRow
                  key={server.id}
                  title={server.name}
                  subtitle={`${status} · ${endpoint}`}
                  leading={
                    <View style={[styles.serverGlyph, { backgroundColor: theme.materials.tint }]}>
                      <Ionicons
                        name="desktop-outline"
                        size={17}
                        color={colors.accentStrong}
                      />
                      <View
                        style={[
                          styles.serverGlyphDot,
                          {
                            borderColor: colors.bgSurface,
                            backgroundColor: connectionIssue
                              ? connectionIssueAccent(connectionIssue, colors)
                              : connectionColor(connectionState, colors),
                          },
                        ]}
                      />
                    </View>
                  }
                  trailing={
                    current ? (
                      <Ionicons
                        name="checkmark-circle"
                        size={20}
                        color={colors.accentStrong}
                        accessibilityElementsHidden
                        importantForAccessibility="no-hide-descendants"
                      />
                    ) : null
                  }
                  accessory="chevron"
                  accessibilityLabel={`${server.name}${current ? ", in use" : ""}, ${connectionLabel(connectionState)}${
                    connectionState === "connected" && latencySample
                      ? `, ${formatLatency(latencySample.latencyMs)} latency`
                      : ""
                  }, ${server.transportKind === "link" ? "Zen Link" : server.url}`}
                  accessibilityHint="Shows actions for this server"
                  onPress={() => setServerMenuId(server.id)}
                />
              );
            })}
            <ListRow
              title="Pair a server"
              icon="add"
              accessibilityLabel="Pair a server"
              accessory="chevron"
              onPress={() => {
                void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                openCreateServer();
              }}
            />
          </ListSection>
          {currentIssue ? (
            <InlineNotice
              tone="danger"
              title={currentIssue.title}
              detail={currentIssue.hint ?? currentIssue.detail}
              style={styles.sectionNotice}
            />
          ) : null}

          <SettingsSectionHeader>Channels</SettingsSectionHeader>
          {currentServerId ? (
            <TelegramConnectionRow
              key={currentServerId || "no-current-server"}
              serverId={currentServerId}
              connected={serverConnections[currentServerId] === "connected"}
            />
          ) : <Text style={styles.emptyText}>No current server</Text>}

          <SettingsSectionHeader>Providers</SettingsSectionHeader>
          <ListSection>
            <ListRow
              title="Models and accounts"
              subtitle={!currentServerId ? "No current server" : null}
              icon="key-outline"
              accessory="chevron"
              accessibilityLabel="Providers"
              accessibilityHint="Manage Provider connections and API keys"
              onPress={() => router.push("/model-profiles")}
            />
          </ListSection>

          <SettingsSectionHeader>Appearance</SettingsSectionHeader>
          <View accessibilityRole="radiogroup" accessibilityLabel="Appearance theme">
            <ListSection>
              {THEME_CHOICES.map((choice) => {
                const selected = preference === choice.value;
                return (
                  <ListRow
                    key={choice.value}
                    title={choice.label}
                    icon={choice.icon}
                    accessory="check"
                    accessibilityRole="radio"
                    accessibilityLabel={`${choice.label} appearance`}
                    selected={selected}
                    onPress={() => void setPreference(choice.value)}
                  />
                );
              })}
            </ListSection>
          </View>

          <SettingsSectionHeader>About</SettingsSectionHeader>
          <ListSection>
            <ListRow
              title="Zen"
              value={`Version ${appVersion}`}
              leading={<ZenLogoMark size={30} accessible={false} />}
            />
          </ListSection>
        </View>
      </ScrollView>

      <ActionMenu
        visible={menuServer !== null}
        title={menuServer?.name}
        onClose={() => setServerMenuId(null)}
        items={menuServer ? serverMenuItems(menuServer) : []}
      />

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
                  <Button label="Cancel" variant="plain" style={styles.modalAction} onPress={closeEditor} />
                  <Button
                    label="Save"
                    variant="filled"
                    style={styles.modalAction}
                    onPress={() => {
                      void handleSaveServer();
                    }}
                  />
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
                  <Button label="Cancel" variant="plain" style={styles.modalAction} onPress={closeEditor} />
                  <Button
                    label="Import"
                    variant="filled"
                    style={styles.modalAction}
                    onPress={() => {
                      void handleImportDraft();
                    }}
                  />
                </View>

                <Button
                  label="Scan QR Code"
                  icon="qr-code-outline"
                  variant="tinted"
                  block
                  style={styles.scanQrAction}
                  onPress={openScanner}
                />
              </>
            )}
          </ScrollView>
        )}
      </RisingSheet>
    </SafeAreaView>
  );
}

function SettingsSectionHeader({ children }: { children: string }) {
  return (
    <AppText
      variant="label"
      tone="tertiary"
      accessibilityRole="header"
      style={settingsHeaderStyles.header}
    >
      {children}
    </AppText>
  );
}

const settingsHeaderStyles = StyleSheet.create({
  header: {
    paddingHorizontal: 16,
    paddingTop: 4,
    paddingBottom: 6,
  },
});

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

  useEffect(() => {
    if (!expanded || !connected || !serverId || busy) return;
    let cancelled = false;
    let pending = false;
    const refresh = async () => {
      if (pending || AppState.currentState !== "active") return;
      pending = true;
      const epoch = ownerEpoch.current;
      try {
        const next = await wsClient.getTelegramConnectionStatus(serverId);
        if (!cancelled && epoch === ownerEpoch.current) setStatus(next);
      } catch { /* The explicit refresh path surfaces connection failures. */ }
      finally { pending = false; }
    };
    const timer = setInterval(() => void refresh(), 5000);
    const subscription = AppState.addEventListener("change", state => {
      if (state === "active") void refresh();
      else { ownerEpoch.current++; setToken(""); }
    });
    return () => { cancelled = true; clearInterval(timer); subscription.remove(); };
  }, [expanded, connected, serverId, busy]);

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
    const epoch = ownerEpoch.current;
    try {
      const value = (await Clipboard.getStringAsync()).trim();
      if (ownerActive.current && epoch === ownerEpoch.current) setToken(value);
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
      if (!status?.enabled) {
        await wsClient.enableTelegramConnection(serverId);
        if (epoch !== ownerEpoch.current) return;
      }
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


  const stateLabel = visibleStatus
    ? telegramConnectionStateLabel(visibleStatus.state)
    : loading
      ? "Loading"
      : setupMode === "local"
        ? "Server offline"
        : "Unavailable";
  const stateColor = visibleStatus
    ? telegramConnectionStateColor(visibleStatus.state, colors)
    : colors.textTertiary;
  const closeDetails = () => {
    ownerEpoch.current++;
    setBusy(false);
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
            <TelegramConnectionPanel
              status={visibleStatus} connected={Boolean(activeServerId)} loading={loading} busy={busy}
              error={loadError} token={token} editingToken={showToken}
              onToken={setToken} onPaste={() => void pasteToken()} onConfigure={() => void configure()}
              onBotFather={() => void openBotFather()} onBind={() => void beginBinding()}
              onOpen={() => { if (visibleStatus?.bot_username) void Linking.openURL(`https://t.me/${visibleStatus.bot_username}`).catch(() => Alert.alert("Telegram unavailable", "The bot chat could not be opened.")); }}
              onReconnect={() => { if (activeServerId) void runStatusMutation(() => wsClient.enableTelegramConnection(activeServerId)); }}
              onDisconnect={() => { if (activeServerId) void runStatusMutation(() => wsClient.disableTelegramConnection(activeServerId)); }}
              onEditToken={() => setShowToken(true)}
              onCancelToken={() => { setToken(""); setShowToken(false); }}
              onRetry={() => setReload(value => value + 1)}
              onRevoke={() => { if (activeServerId) confirmMutation("Unlink Telegram account",
                "Remove the verified Telegram owner and require a new binding?", "Unlink",
                () => wsClient.revokeTelegramOwner(activeServerId)); }}
              onRemove={() => { if (activeServerId) confirmMutation("Remove Telegram bot",
                "Remove this server's token, binding and delivery state? Telegram cloud messages are not deleted.", "Remove",
                () => wsClient.removeTelegramConnection(activeServerId)); }}
            />
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
  colors: AppColors,
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
  colors: AppColors,
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
    serverGlyph: {
      width: 30,
      height: 30,
      borderRadius: 9,
      ...ContinuousCorners,
      alignItems: "center",
      justifyContent: "center",
    },
    serverGlyphDot: {
      position: "absolute",
      right: -3,
      bottom: -3,
      width: 11,
      height: 11,
      borderRadius: 6,
      borderWidth: 2,
    },
    sectionNotice: {
      marginTop: -14,
      marginBottom: 26,
    },


    // Matches ListSection's grouped card so Channels sits in the same rhythm.
    serverList: {
      overflow: "hidden",
      borderRadius: Radii.card,
      ...ContinuousCorners,
      marginBottom: 26,
      backgroundColor: colors.bgSurface,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
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
    emptyText: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textPrimary,
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
    modalAction: {
      flex: 1,
    },
    scanQrAction: {
      marginTop: 12,
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
