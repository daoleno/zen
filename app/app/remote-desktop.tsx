import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert, AppState, KeyboardAvoidingView, PanResponder, Platform, Pressable, StyleSheet, Switch, Text, TextInput, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Stack, router, useFocusEffect } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { useCurrentServer } from "../store/currentServer";
import { useAppColors } from "../constants/tokens";
import { prepareDesktopConnection } from "../services/remoteDesktop";
import { desktopKey, desktopPoint, desktopTextEdits, desktopStart, beginDesktopPan, advanceDesktopPan, type DesktopInput, type DesktopPanState } from "../services/remoteDesktopModel";
import { NativeDesktopView, type DesktopState } from "../modules/zen-remote-desktop/src";
import { DesktopCommandQueue, type DesktopCommandTarget } from "../services/remoteDesktopCommands";
import { desktopLanOrigin, hasDesktopLanConsent } from "../services/desktopTransportPolicy";
import { setDesktopLanConsent } from "../services/storage";
import { DesktopConnectionUnavailable, DesktopPreflightError } from "../services/desktopConnectionCheck";
import { PAIRING_SCOPE_COPY } from "../services/pairingScope";

export default function RemoteDesktopScreen() {
  const { currentServer } = useCurrentServer();
  return <DesktopSession key={currentServer ? JSON.stringify([currentServer.id, currentServer.url, currentServer.daemonId,
    currentServer.daemonPublicKey, currentServer.transportKind, currentServer.transportPin, currentServer.linkRouteId]) : "none"} />;
}

function DesktopSession() {
  const { currentServer, isCurrentServer, refreshServers } = useCurrentServer();
  const colors = useAppColors();
  const root = useRef<View>(null);
  const [headerHeight, setHeaderHeight] = useState(0);
  const [connection, setConnection] = useState("");
  const [status, setStatus] = useState<DesktopState>({ state: "disconnected" });
  const [preparing, setPreparing] = useState(false);
  const [control, setControl] = useState(false);
  const [panMode, setPanMode] = useState(false);
  const [dragMode, setDragMode] = useState(false);
  const [keyboard, setKeyboard] = useState(false);
  const [text, setText] = useState("");
  const textRef = useRef("");
  const [zoom, setZoom] = useState(1);
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const [size, setSize] = useState({ width: 1, height: 1 });
  const generation = useRef(0);
  const pending = useRef<AbortController | null>(null);
  const reconnect = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectAttempts = useRef(0);
  const reconnectAllowed = useRef(false);
  const [transport, setTransport] = useState("");
  const native = useRef<DesktopCommandTarget>(null);
  const [commands] = useState(() => new DesktopCommandQueue(() => native.current, (reason) => {
    generation.current++;
    pending.current?.abort();
    reconnectAllowed.current = false;
    if (reconnect.current) clearTimeout(reconnect.current);
    reconnect.current = null;
    textRef.current = ""; setText("");
    setConnection(""); setPreparing(false); setControl(false); setKeyboard(false);
    setStatus({ state: "disconnected", reason });
  }));
  const inputGeneration = commands.currentGeneration;
  const gestureStart = useRef<{
    x: number; y: number; time: number; pinch: number; zoom: number;
    offset: { x: number; y: number }; pan: DesktopPanState | null;
  }>({ x: 0, y: 0, time: 0, pinch: 0, zoom: 1, offset: { x: 0, y: 0 }, pan: null });
  const connected = status.state === "connected";
  const lan = currentServer ? desktopLanOrigin(currentServer) : null;
  const lanAllowed = currentServer ? hasDesktopLanConsent(currentServer) : false;
  const send = (value: object) => commands.send(value);
  const input = (events: DesktopInput[]) => {
    if (connected && control && events.length) send({ type: "batch", events });
  };
  const stop = useCallback((keepReconnect = false) => {
    reconnectAllowed.current = keepReconnect;
    if (!keepReconnect) reconnectAttempts.current = 0;
    if (reconnect.current) clearTimeout(reconnect.current);
    reconnect.current = null;
    generation.current++;
    pending.current?.abort(); pending.current = null;
    commands.stop();
    setTransport("");
    setConnection(""); setPreparing(false); setControl(false);
    setStatus({ state: "disconnected" }); setKeyboard(false);
    textRef.current = ""; setText("");
    setDragMode(false); setPanMode(false); setZoom(1); setOffset({ x: 0, y: 0 });
  }, [commands]);
  useFocusEffect(useCallback(() => () => stop(), [stop]));
  useEffect(() => {
    const subscription = AppState.addEventListener("change", (state) => { if (state !== "active") stop(); });
    return () => { stop(); subscription.remove(); };
  }, [commands, stop]);
  const scheduleReconnect = () => {
    if (!reconnectAllowed.current || reconnectAttempts.current >= 6) return;
    const epoch = generation.current;
    const delay = Math.min(30000, 1500 * 2 ** reconnectAttempts.current++);
    reconnect.current = setTimeout(() => {
      if (reconnectAllowed.current && epoch === generation.current && currentServer && isCurrentServer(currentServer.id)) void connect(true);
    }, delay);
  };
  const connect = async (automatic = false) => {
    if (!currentServer) return;
    if (!automatic) reconnectAttempts.current = 0;
    reconnectAllowed.current = true;
    const epoch = ++generation.current;
    const inputGeneration = commands.begin();
    setConnection(""); setControl(false);
    setPreparing(true); setStatus({ state: "disconnected" });
    try {
      const server = currentServer;
      pending.current?.abort(); pending.current = new AbortController();
      const next = await prepareDesktopConnection(server, inputGeneration, pending.current.signal);
      if (epoch === generation.current && isCurrentServer(server.id)) {
        setTransport(JSON.parse(next).transport); setConnection(next);
      }
    } catch (error) {
      if (epoch === generation.current) {
        reconnectAllowed.current = automatic && error instanceof DesktopConnectionUnavailable;
        commands.stop();
        setStatus({
          state: "disconnected",
          reason: error instanceof DesktopPreflightError
            ? (error.recovery || error.message)
            : error instanceof Error ? error.message : "Connection failed.",
        });
        scheduleReconnect();
      }
    } finally { if (epoch === generation.current) setPreparing(false); }
  };
  const grantDesktop = () => {
    Alert.alert("Grant unattended desktop", PAIRING_SCOPE_COPY,
      [{ text: "Cancel", style: "cancel" },
        { text: "Scan pairing link", onPress: () => router.push({ pathname: "/settings", params: { pairMode: "scanner" } }) }]);
  };
  const acknowledgeAttendedLan = async () => {
    if (!currentServer) return;
    const origin = desktopLanOrigin(currentServer);
    if (!origin) return;
    const allowed = await new Promise<boolean>((resolve) => Alert.alert(
      "Attended assistance only",
      `Unencrypted LAN cannot open lock or login screens or carry OS passwords. Allow attended assistance to ${origin} only on a network you trust.`,
      [{ text: "Cancel", style: "cancel", onPress: () => resolve(false) },
        { text: "Allow attended LAN", style: "destructive", onPress: () => resolve(true) }],
      { cancelable: true, onDismiss: () => resolve(false) },
    ));
    if (!allowed || !isCurrentServer(currentServer.id)) return;
    await setDesktopLanConsent(currentServer, true, () => isCurrentServer(currentServer.id));
    await refreshServers();
  };
  const revokeLan = async () => {
    if (!currentServer) return;
    const server = currentServer; stop();
    try { await setDesktopLanConsent(server, false, () => isCurrentServer(server.id)); await refreshServers(); }
    catch (error) { if (isCurrentServer(server.id)) setStatus({ state: "disconnected", reason: error instanceof Error ? error.message : "Unable to update desktop approval." }); }
  };
  const pointer = (x: number, y: number) => desktopPoint(
    (x - size.width / 2 - offset.x) / zoom + size.width / 2,
    (y - size.height / 2 - offset.y) / zoom + size.height / 2,
    size.width, size.height, status.width ?? 1280, status.height ?? 720,
  );
  const responder = PanResponder.create({
    onStartShouldSetPanResponder: () => connected,
    onMoveShouldSetPanResponder: () => connected,
    onPanResponderGrant: (event) => {
      const touches = event.nativeEvent.touches;
      gestureStart.current = { x: event.nativeEvent.locationX, y: event.nativeEvent.locationY, time: Date.now(),
        pinch: touches.length === 2 ? Math.hypot(touches[0].pageX - touches[1].pageX, touches[0].pageY - touches[1].pageY) : 0,
        zoom, offset, pan: null };
      if (dragMode && !panMode && touches.length === 1) {
        const point = pointer(event.nativeEvent.locationX, event.nativeEvent.locationY);
        if (point) input([{ type: "pointer", ...point }, { type: "button", code: 1, down: true }]);
      }
    },
    onPanResponderMove: (event, gesture) => {
      const touches = event.nativeEvent.touches;
      if (touches.length === 2) {
        if (!gestureStart.current.pinch && dragMode) input([{ type: "release" }]);
        const distance = Math.hypot(touches[0].pageX - touches[1].pageX, touches[0].pageY - touches[1].pageY);
        if (gestureStart.current.pinch === 0) gestureStart.current.pinch = distance;
        setZoom(Math.max(1, Math.min(4, gestureStart.current.zoom * distance / gestureStart.current.pinch)));
      } else if (panMode) {
        // The responder is recreated on each offset render, so accumulate pan
        // state in the persistent ref; per-instance gestureState.dx would only
        // ever reflect the last fragment.
        const start = gestureStart.current;
        const point = { x: event.nativeEvent.pageX, y: event.nativeEvent.pageY };
        const pan = start.pan ? advanceDesktopPan(start.pan, point) : beginDesktopPan(start.offset, point);
        start.pan = pan;
        start.offset = pan.offset;
        setOffset(pan.offset);
      } else {
        const point = pointer(event.nativeEvent.locationX, event.nativeEvent.locationY);
        if (point) input([{ type: "pointer", ...point }]);
      }
    },
    onPanResponderRelease: (event, gesture) => {
      if (dragMode) input([{ type: "release" }]);
      gestureStart.current.pan = null;
      if (!dragMode && !panMode && !gestureStart.current.pinch && Math.hypot(gesture.dx, gesture.dy) < 10) {
        const point = pointer(event.nativeEvent.locationX, event.nativeEvent.locationY);
        const code = Date.now() - gestureStart.current.time > 550 ? 3 : 1;
        if (point) input([{ type: "pointer", ...point }, { type: "button", code, down: true }, { type: "button", code, down: false }]);
      }
    },
    onPanResponderTerminate: () => { gestureStart.current.pan = null; input([{ type: "release" }]); },
  });
  const tool = (icon: React.ComponentProps<typeof Ionicons>["name"], label: string, action: () => void, selected = false, disabled = false) => (
    <Pressable key={label} accessibilityRole="button" accessibilityLabel={label} accessibilityState={{ selected, disabled }}
      disabled={disabled} onPress={action} style={[styles.tool, { backgroundColor: selected ? colors.surfacePressed : "transparent", opacity: disabled ? 0.35 : 1 }]}>
      <Ionicons name={icon} size={22} color={colors.textPrimary} />
    </Pressable>
  );
  return <SafeAreaView ref={root} onLayout={() => root.current?.measureInWindow((_x, y) => setHeaderHeight(y))}
    edges={["bottom"]} style={[styles.root, { backgroundColor: colors.surfaceSubtle }]}>
    <Stack.Screen options={{ title: "Remote Desktop", headerShown: true }} />
    <KeyboardAvoidingView style={styles.root} behavior={Platform.OS === "ios" ? "padding" : "height"} keyboardVerticalOffset={headerHeight}>
    <View style={styles.header}>
      <Text numberOfLines={1} style={[styles.host, { color: colors.textPrimary }]}>{currentServer?.name ?? "No current server"}</Text>
      <Text style={{ color: colors.textSecondary }}>{preparing ? "Connecting" : status.state === "streaming" ? "Waiting for video" : status.state === "requesting" ? "Awaiting permission" : connected ? transport === "trusted-lan" ? "Connected (unencrypted attended LAN)" : "Connected" : ""}</Text>
      {lan ? <View style={styles.lanRow}>
        <Text style={{ color: colors.textSecondary, flex: 1 }}>Attended unencrypted LAN (not lock/login)</Text>
        <Switch accessibilityLabel="Attended unencrypted LAN desktop" value={lanAllowed} disabled={preparing}
          onValueChange={(allowed) => { if (allowed) void acknowledgeAttendedLan(); else void revokeLan(); }} />
      </View> : null}
    </View>
    <View style={styles.viewport} onLayout={(event) => setSize(event.nativeEvent.layout)}>
      {connection ? <View style={[StyleSheet.absoluteFill, { transform: [{ translateX: offset.x }, { translateY: offset.y }, { scale: zoom }] }]}>
        <NativeDesktopView ref={native} key={connection} style={styles.root} connection={connection} onState={({ nativeEvent }) => {
          if (commands.currentGeneration !== inputGeneration) return;
          if (["disconnected", "denied", "unsupported"].includes(nativeEvent.state)) {
            const retry = nativeEvent.state === "disconnected" && reconnectAllowed.current && reconnectAttempts.current < 6;
            stop(retry);
            if (retry) scheduleReconnect();
          }
          if (nativeEvent.state === "connected") reconnectAttempts.current = 0;
          if (nativeEvent.state === "streaming" || nativeEvent.state === "connected") setControl(nativeEvent.control === true);
          if (nativeEvent.surface === "greeter" || nativeEvent.surface === "locked") {
            textRef.current = ""; setText(""); setKeyboard(false);
          }
          setStatus(nativeEvent);
        }} />
      </View> : null}
      {connected ? <View style={StyleSheet.absoluteFill} {...responder.panHandlers} /> : <View style={styles.empty}>
        <Ionicons name="desktop-outline" size={40} color="#b9bec5" />
        <Text style={styles.message}>{status.reason || (status.state === "sources" ? status.source === "wayland" ? "Wayland portal desktop" : "Selected X11 desktop" : status.state === "requesting" ? "Awaiting host permission" : status.state === "streaming" ? "Waiting for video" : preparing ? "Connecting" : "Connect")}</Text>
        {status.state === "sources" ? <>
          <View style={styles.mode}><Text style={styles.message}>Allow control</Text><Switch value={control} onValueChange={setControl} /></View>
          <Pressable accessibilityRole="button" disabled={!desktopStart(status.source, control)} onPress={() => {
            const start = desktopStart(status.source, control);
            if (start) send(start);
          }} style={styles.action}><Text style={styles.actionText}>Share desktop</Text></Pressable>
        </> : !preparing && !["requesting", "streaming"].includes(status.state) ? <>
          <Pressable accessibilityRole="button" disabled={!currentServer} onPress={() => void connect()} style={styles.action}><Text style={styles.actionText}>Connect</Text></Pressable>
          {status.reason?.includes("terminal access only") ? <Pressable accessibilityRole="button" onPress={grantDesktop} style={styles.action}><Text style={styles.actionText}>Grant unattended desktop</Text></Pressable> : null}
        </> : null}
      </View>}
    </View>
    <View style={[styles.toolbar, { borderColor: colors.borderSubtle }]}>
      {tool("stop-circle-outline", "Stop desktop", () => stop(), false, !connection && !preparing && reconnect.current === null)}
      {tool("hand-left-outline", "Pan desktop", () => { setPanMode(!panMode); setDragMode(false); }, panMode, !connected)}
      {tool("move-outline", "Drag pointer", () => { setDragMode(!dragMode); setPanMode(false); }, dragMode, !connected || !control)}
      {tool("contract-outline", "Reset zoom", () => { setZoom(1); setOffset({ x: 0, y: 0 }); }, false, !connected)}
      {tool("keypad-outline", "Keyboard", () => { textRef.current = ""; setText(""); setKeyboard(!keyboard); }, keyboard, !connected || !control || status.surface === "greeter" || status.surface === "locked")}
      {tool("lock-closed-outline", "OS password", () => {
        textRef.current = ""; setText(""); setKeyboard(false);
        void native.current?.showSensitiveInput?.(commands.currentGeneration).catch(() => undefined);
      }, false, !connected || !status.sensitiveInput)}
      {tool("chevron-up-outline", "Scroll up", () => input([{ type: "scroll", delta: -3 }]), false, !connected || !control)}
      {tool("chevron-down-outline", "Scroll down", () => input([{ type: "scroll", delta: 3 }]), false, !connected || !control)}
    </View>
    {keyboard ? <View style={styles.keyboard}>
      <TextInput autoFocus value={text} maxLength={1024} onChangeText={(value) => {
        const batches = desktopTextEdits(textRef.current, value);
        textRef.current = value; setText(value);
        for (const events of batches) input(events);
      }}
        onSubmitEditing={() => input(desktopKey(0xff0d))} autoCorrect={false} autoCapitalize="none" placeholder="Type" accessibilityLabel="Desktop keyboard"
        style={[styles.textInput, { color: colors.textPrimary, borderColor: colors.borderSubtle }]} />
      {tool("return-down-back-outline", "Enter", () => input(desktopKey(0xff0d)))}
      {tool("arrow-back-outline", "Backspace", () => input(desktopKey(0xff08)))}
    </View> : null}
    </KeyboardAvoidingView>
  </SafeAreaView>;
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  header: { paddingHorizontal: 16, paddingVertical: 10, gap: 4 },
  lanRow: { flexDirection: "row", alignItems: "center", gap: 12 },
  host: { fontSize: 14, fontWeight: "600" },
  viewport: { flex: 1, backgroundColor: "#000000", overflow: "hidden" },
  empty: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 16 },
  message: { fontSize: 15, color: "#e3e7eb", textAlign: "center" },
  mode: { flexDirection: "row", alignItems: "center", gap: 16 },
  action: { backgroundColor: "#edf1f4", paddingHorizontal: 20, paddingVertical: 12, borderRadius: 6 },
  actionText: { color: "#182024", fontSize: 15, fontWeight: "600" },
  toolbar: { flexDirection: "row", flexWrap: "wrap", justifyContent: "space-evenly", borderTopWidth: StyleSheet.hairlineWidth, padding: 4 },
  tool: { width: 44, height: 44, alignItems: "center", justifyContent: "center", borderRadius: 6 },
  keyboard: { flexDirection: "row", padding: 8, alignItems: "center" },
  textInput: { flex: 1, minWidth: 0, height: 44, borderWidth: 1, borderRadius: 6, paddingHorizontal: 12 },
});
