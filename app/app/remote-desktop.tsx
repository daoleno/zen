import React, { useCallback, useEffect, useRef, useState } from "react";
import { AppState, Keyboard, KeyboardAvoidingView, PanResponder, Platform, Pressable, StyleSheet, Text, TextInput, useWindowDimensions, View } from "react-native";
import { Ionicons, MaterialIcons } from "@expo/vector-icons";
import { Stack, router, useFocusEffect } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { useCurrentServer } from "../store/currentServer";
import { useAppColors } from "../constants/tokens";
import { prepareDesktopConnection } from "../services/remoteDesktop";
import { enableDesktopScope } from "../services/desktopScopeGrant";
import { confirmDesktopEnable } from "../services/confirmDesktopEnable";
import { desktopNamedKey, desktopPoint, desktopCommittedText, desktopTextEdits, beginDesktopPan, advanceDesktopPan, type DesktopInput, type DesktopNamedKey, type DesktopPanState } from "../services/remoteDesktopModel";
import { NativeDesktopView, NativeMoonlightDesktopView, NativeDesktopKeyboard, type DesktopKeyboardApi, type DesktopState, type MoonlightDesktopApi, type MoonlightDesktopState } from "../modules/zen-remote-desktop/src";
import { DesktopCommandQueue, type DesktopCommandTarget } from "../services/remoteDesktopCommands";
import { DesktopConnectionUnavailable, DesktopPreflightError, type MoonlightHostBootstrap } from "../services/desktopConnectionCheck";

export default function RemoteDesktopScreen() {
  const { currentServer } = useCurrentServer();
  return <DesktopSession key={currentServer ? JSON.stringify([currentServer.id, currentServer.url, currentServer.daemonId,
    currentServer.daemonPublicKey, currentServer.transportKind, currentServer.transportPin, currentServer.linkRouteId]) : "none"} />;
}

function DesktopSession() {
  const { currentServer, isCurrentServer } = useCurrentServer();
  const colors = useAppColors();
  const root = useRef<View>(null);
  const [headerHeight, setHeaderHeight] = useState(0);
  const [connection, setConnection] = useState("");
  const [status, setStatus] = useState<DesktopState>({ state: "disconnected" });
  const [preparing, setPreparing] = useState(false);
  const [preflightCode, setPreflightCode] = useState("");
  const [enabling, setEnabling] = useState(false);
  const [control, setControl] = useState(false);
  const [panMode, setPanMode] = useState(false);
  const [dragMode, setDragMode] = useState(false);
  const [keyboard, setKeyboard] = useState(false);
  const [inputNotice, setInputNotice] = useState("");
  const [text, setText] = useState("");
  const textRef = useRef("");
  const [zoom, setZoom] = useState(1);
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const [size, setSize] = useState({ width: 1, height: 1 });
  const nativeKeyboard = useRef<DesktopKeyboardApi>(null);
  const textInput = useRef<TextInput>(null);
  const { width: windowWidth, height: windowHeight } = useWindowDimensions();
  const landscape = windowWidth > windowHeight;
  // In landscape the soft keyboard is tall (about 63% of the screen). The stack
  // header and server row would leave the remote video a few pixels, so the
  // route hides them while the keyboard is open and keeps the toolbar plus the
  // video above the docked keyboard.
  const compactLandscape = landscape && keyboard;
  const generation = useRef(0);
  const pending = useRef<AbortController | null>(null);
  const grantPending = useRef<AbortController | null>(null);
  const reconnect = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectAttempts = useRef(0);
  const reconnectAllowed = useRef(false);
  const [transport, setTransport] = useState("");
  const native = useRef<DesktopCommandTarget>(null);
  const moonlight = useRef<MoonlightDesktopApi>(null);
  const moonlightSession = useRef<{ generation: number; ended: boolean } | null>(null);
  const moonlightPoint = useRef({ x: 0, y: 0 });
  const moonlightButtons = useRef(new Set<number>());
  const [moonlightPin, setMoonlightPin] = useState("");
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
  const textInputAllowed = connected && control && status.surface !== "greeter" && status.surface !== "locked";
  const send = (value: object) => commands.send(value);
  // Moonlight route: pointer deltas, buttons and committed text go to the
  // native view on the connection that produced the state event.
  const moonlightInput = (events: DesktopInput[]) => {
    const view = moonlight.current;
    const session = moonlightSession.current;
    if (!view || !session || session.ended) return;
    for (const event of events) {
      if (event.type === "pointer") {
        const dx = Math.round(event.x - moonlightPoint.current.x);
        const dy = Math.round(event.y - moonlightPoint.current.y);
        moonlightPoint.current = { x: event.x, y: event.y };
        if (dx || dy) void view.sendPointerMove(session.generation, dx, dy).catch(() => undefined);
      } else if (event.type === "button") {
        if (event.down) moonlightButtons.current.add(event.code);
        else moonlightButtons.current.delete(event.code);
        void view.sendPointerButton(session.generation, event.code, event.down ? 7 : 8).catch(() => undefined);
      } else if (event.type === "release") {
        for (const code of moonlightButtons.current) void view.sendPointerButton(session.generation, code, 8).catch(() => undefined);
        moonlightButtons.current.clear();
      }
    }
  };
  const input = (events: DesktopInput[]) => {
    if (moonlight.current) return moonlightInput(events);
    if (connected && control && events.length) send({ type: "batch", events });
  };
  const sendCommittedText = (value: string) => {
    const session = moonlightSession.current;
    if (moonlight.current) {
      if (session && !session.ended) void moonlight.current.sendText(session.generation, value).catch(() => undefined);
      return;
    }
    // The daemon rejects batches above 64 events, so a paste or IME commit is
    // split here and a key press always travels with its release.
    for (const batch of desktopCommittedText(value)) input(batch);
  };
  const moonlightNamedKey = (key: DesktopNamedKey, down: boolean) => {
    const view = moonlight.current;
    const session = moonlightSession.current;
    if (!view || !session || session.ended) return;
    const code = key === "Backspace" ? 8 : key === "Delete" ? 46 : 13;
    void view.sendKey(session.generation, code, down ? 3 : 4, 0, 0).catch(() => undefined);
  };
  const sendNamedKey = (key: DesktopNamedKey) => {
    if (moonlight.current) {
      moonlightNamedKey(key, true);
      moonlightNamedKey(key, false);
      return;
    }
    input(desktopNamedKey(key));
  };
  const stop = useCallback((keepReconnect = false) => {
    const session = moonlightSession.current;
    if (session && !session.ended) {
      const view = moonlight.current;
      if (view) {
        if (keepReconnect) void view.disconnect(session.generation).catch(() => undefined);
        else void view.revoke(session.generation).catch(() => undefined);
      }
      session.ended = true;
      moonlightSession.current = null;
    }
    moonlightButtons.current.clear();
    setMoonlightPin("");
    reconnectAllowed.current = keepReconnect;
    if (!keepReconnect) reconnectAttempts.current = 0;
    if (reconnect.current) clearTimeout(reconnect.current);
    reconnect.current = null;
    generation.current++;
    pending.current?.abort(); pending.current = null;
    commands.stop();
    setTransport("");
    setConnection(""); setPreparing(false); setControl(false);
    setStatus({ state: "disconnected" }); setKeyboard(false); setInputNotice("");
    textRef.current = ""; setText("");
    setDragMode(false); setPanMode(false); setZoom(1); setOffset({ x: 0, y: 0 });
    setPreflightCode("");
    grantPending.current?.abort(); grantPending.current = null;
    setEnabling(false);
  }, [commands]);
  useFocusEffect(useCallback(() => () => stop(true), [stop]));
  useEffect(() => {
    const subscription = AppState.addEventListener("change", (state) => { if (state !== "active") stop(true); });
    return () => { stop(true); subscription.remove(); };
  }, [commands, stop]);
  // Opening the input bar focuses the commit-aware native field when present;
  // otherwise the plain TextInput keeps the pre-native shell working. The OS
  // keyboard dismiss (back gesture) closes the bar instead of leaving it open.
  useEffect(() => {
    if (!keyboard) return;
    const focus = setTimeout(() => {
      if (NativeDesktopKeyboard) void nativeKeyboard.current?.focus?.().catch(() => undefined);
      else textInput.current?.focus();
    }, 50);
    const hidden = Keyboard.addListener("keyboardDidHide", () => setKeyboard(false));
    return () => { clearTimeout(focus); hidden.remove(); };
  }, [keyboard]);
  useEffect(() => {
    if (!inputNotice) return;
    const timer = setTimeout(() => setInputNotice(""), 5000);
    return () => clearTimeout(timer);
  }, [inputNotice]);
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
    if (reconnect.current) clearTimeout(reconnect.current);
    reconnect.current = null;
    if (!automatic) reconnectAttempts.current = 0;
    reconnectAllowed.current = true;
    const epoch = ++generation.current;
    const inputGeneration = commands.begin();
    setConnection(""); setControl(false);
    setPreparing(true); setStatus({ state: "disconnected" });
    setPreflightCode("");
    try {
      const server = currentServer;
      pending.current?.abort(); pending.current = new AbortController();
      const next = await prepareDesktopConnection(server, inputGeneration, pending.current.signal);
      if (epoch === generation.current && isCurrentServer(server.id)) {
        const parsed = JSON.parse(next) as { transport?: string; moonlight?: MoonlightHostBootstrap };
        if (parsed.transport === "moonlight" && parsed.moonlight) {
          if (!NativeMoonlightDesktopView) throw new Error("This build has no Moonlight desktop view.");
          // The client generates the pairing PIN; the host operator enters it
          // in Sunshine's Web UI for the matching request.
          const pin = String(Math.floor(1000 + Math.random() * 9000));
          moonlightSession.current = null;
          setMoonlightPin(pin);
          setTransport("moonlight");
          setConnection(JSON.stringify({ ...parsed.moonlight, pin }));
        } else {
          setMoonlightPin("");
          setTransport(parsed.transport ?? "");
          setConnection(next);
        }
      }
    } catch (error) {
      if (epoch === generation.current) {
        reconnectAllowed.current = automatic && error instanceof DesktopConnectionUnavailable;
        commands.stop();
        setPreflightCode(error instanceof DesktopPreflightError ? error.code : "");
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
  const pairDesktop = () => router.push({ pathname: "/settings", params: { addServer: Date.now().toString(), pairMode: "scanner" } });
  const enableDesktop = async () => {
    if (!currentServer || enabling) return;
    const server = currentServer;
    const epoch = generation.current;
    if (!await confirmDesktopEnable(server.name || "this computer")) return;
    if (epoch !== generation.current || !isCurrentServer(server.id)) return;
    grantPending.current?.abort();
    const controller = new AbortController();
    grantPending.current = controller;
    setEnabling(true);
    setStatus({ state: "disconnected" });
    setPreflightCode("");
    try {
      await enableDesktopScope(server, controller.signal);
      if (epoch !== generation.current || controller.signal.aborted || !isCurrentServer(server.id)) return;
      grantPending.current = null;
      setEnabling(false);
      await connect();
    } catch (error) {
      if (epoch !== generation.current || controller.signal.aborted) return;
      grantPending.current = null;
      setEnabling(false);
      setPreflightCode("desktop_scope_required");
      setStatus({ state: "disconnected", reason: error instanceof Error ? error.message : "Could not enable remote desktop." });
    }
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
  const toolNode = (graphic: React.ReactNode, label: string, action: () => void, selected = false, disabled = false, hint?: string) => (
    <Pressable key={label} accessibilityRole="button" accessibilityLabel={label} accessibilityHint={hint} accessibilityState={{ selected, disabled }}
      disabled={disabled} onPress={action} style={[styles.tool, { backgroundColor: selected ? colors.surfacePressed : "transparent", opacity: disabled ? 0.35 : 1 }]}>
      {graphic}
    </Pressable>
  );
  const tool = (icon: React.ComponentProps<typeof Ionicons>["name"], label: string, action: () => void, selected = false, disabled = false, hint?: string) =>
    toolNode(<Ionicons name={icon} size={22} color={colors.textPrimary} />, label, action, selected, disabled, hint);
  return <SafeAreaView ref={root} onLayout={() => root.current?.measureInWindow((_x, y) => setHeaderHeight(y))}
    edges={["bottom"]} style={[styles.root, { backgroundColor: colors.surfaceSubtle }]}>
    <Stack.Screen options={{ title: "Remote Desktop", headerShown: !compactLandscape }} />
    <KeyboardAvoidingView style={styles.root} behavior={Platform.OS === "ios" ? "padding" : "height"} keyboardVerticalOffset={headerHeight}>
    {compactLandscape ? null : <View style={[styles.header, landscape && styles.headerLandscape]}>
      <Text numberOfLines={1} style={[styles.host, landscape && styles.hostLandscape, { color: colors.textPrimary }]}>{currentServer?.name ?? "No current server"}</Text>
      <Text numberOfLines={1} style={{ color: colors.textSecondary }}>{moonlightPin ? `Pair code ${moonlightPin} - enter it in Sunshine` : preparing ? "Connecting" : enabling ? "Enabling remote desktop" : status.state === "streaming" ? "Waiting for video" : status.state === "requesting" ? "Awaiting permission" : connected ? transport === "trusted-lan" ? "Connected (unencrypted attended LAN)" : "Connected" : ""}</Text>
    </View>}
    <View style={styles.viewport} onLayout={(event) => setSize(event.nativeEvent.layout)}>
      {connection ? <View style={[StyleSheet.absoluteFill, { transform: [{ translateX: offset.x }, { translateY: offset.y }, { scale: zoom }] }]}>
        {transport === "moonlight" && NativeMoonlightDesktopView ? (
          <NativeMoonlightDesktopView ref={moonlight} key={connection} style={styles.root} connection={connection} onState={({ nativeEvent }) => {
            const event = nativeEvent as MoonlightDesktopState & { generation?: number };
            const generationValue = event.generation ?? 0;
            if (generationValue > 0) {
              const current = moonlightSession.current;
              if (!current || current.ended || current.generation <= generationValue) {
                moonlightSession.current = { generation: generationValue, ended: false };
              }
            }
            if (event.state === "connected") { reconnectAttempts.current = 0; setControl(true); }
            if (event.state === "rejected" || event.state === "failed") setInputNotice(event.reason ?? "");
            if (event.state === "disconnected" || event.state === "revoked") {
              const current = moonlightSession.current;
              if (current) current.ended = true;
              moonlightSession.current = null;
              setMoonlightPin("");
              setConnection(""); setTransport(""); setControl(false);
              setStatus({ state: "disconnected", reason: event.reason });
              return;
            }
            if (event.state === "connected" || event.state === "frame") {
              setStatus({ state: "connected", width: event.width, height: event.height, presented: event.presented, dropped: event.dropped });
            } else if (event.state === "connecting" || event.state === "start_accepted" || event.state === "start_failed") {
              setStatus({ state: "requesting" });
            } else if (event.state === "rejected" || event.state === "failed") {
              setStatus({ state: "disconnected", reason: event.reason });
            }
          }} />
        ) : <NativeDesktopView ref={native} key={connection} style={styles.root} connection={connection} onState={({ nativeEvent }) => {
          if (!inputGeneration || commands.currentGeneration !== inputGeneration) return;
          if (nativeEvent.state === "sources") {
            stop();
            setStatus({ state: "unsupported", reason: "This server requested attended sharing. Update Zen on the computer." });
            return;
          }
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
          if (nativeEvent.inputError) setInputNotice(nativeEvent.inputError);
          else if (nativeEvent.state !== "streaming") setInputNotice("");
          setStatus(nativeEvent);
        }} />}
      </View> : null}
      {connected ? <View style={StyleSheet.absoluteFill} {...responder.panHandlers} /> : <View style={styles.empty}>
        <Ionicons name="desktop-outline" size={40} color="#b9bec5" />
        {status.reason ? <Text style={styles.message}>{status.reason}</Text> : null}
        {!preparing && !enabling && !["requesting", "streaming", "sources"].includes(status.state) ? (preflightCode === "desktop_scope_required" ?
          <Pressable accessibilityRole="button" onPress={() => void enableDesktop()} style={styles.action}><Text style={styles.actionText}>Enable remote desktop</Text></Pressable>
          : preflightCode === "device_revoked" ?
          <Pressable accessibilityRole="button" onPress={pairDesktop} style={styles.action}><Text style={styles.actionText}>Pair this phone</Text></Pressable>
          : <Pressable accessibilityRole="button" disabled={!currentServer} onPress={() => void connect()} style={styles.action}><Text style={styles.actionText}>Connect</Text></Pressable>) : null}
      </View>}
    </View>
    <View style={[styles.toolbar, { borderColor: colors.borderSubtle }]}>
      {tool("stop-circle-outline", "Stop desktop", () => stop(), false, !connection && !preparing && reconnect.current === null)}
      {tool("hand-left-outline", "Pan desktop", () => { setPanMode(!panMode); setDragMode(false); }, panMode, !connected)}
      {tool("move-outline", "Drag pointer", () => { setDragMode(!dragMode); setPanMode(false); }, dragMode, !connected || !control)}
      {tool("contract-outline", "Reset zoom", () => { setZoom(1); setOffset({ x: 0, y: 0 }); }, false, !connected)}
      {size.width < 360 ? <View style={styles.toolbarBreak} /> : null}
      {toolNode(<MaterialIcons name="keyboard" size={22} color={colors.textPrimary} />, "Keyboard", () => { setKeyboard(!keyboard); }, keyboard, !textInputAllowed,
        status.surface === "greeter" ? "The login screen uses the OS password action." : status.surface === "locked" ? "The lock screen uses the OS password action." : "Opens the phone keyboard for the focused remote text field.")}
      {tool("lock-closed-outline", "OS password", () => {
        textRef.current = ""; setText(""); setKeyboard(false);
        void native.current?.showSensitiveInput?.(commands.currentGeneration).catch(() => undefined);
      }, false, !connected || !status.sensitiveInput)}
      {tool("chevron-up-outline", "Scroll up", () => input([{ type: "scroll", delta: -3 }]), false, !connected || !control)}
      {tool("chevron-down-outline", "Scroll down", () => input([{ type: "scroll", delta: 3 }]), false, !connected || !control)}
    </View>
    {keyboard ? <View style={[styles.keyboard, compactLandscape && styles.keyboardCompact]}>
      {NativeDesktopKeyboard ? (
        <NativeDesktopKeyboard ref={nativeKeyboard}
          style={[styles.textInput, compactLandscape && styles.textInputCompact, { borderColor: colors.borderSubtle }]}
          onDesktopText={({ nativeEvent }) => sendCommittedText(nativeEvent.value)}
          onDesktopKey={({ nativeEvent }) => sendNamedKey(nativeEvent.key)} />
      ) : (
        <TextInput ref={textInput} autoFocus value={text} maxLength={1024} onChangeText={(value) => {
          const batches = desktopTextEdits(textRef.current, value);
          textRef.current = value; setText(value);
          for (const events of batches) input(events);
        }}
          onSubmitEditing={() => sendNamedKey("Enter")} autoCorrect={false} autoCapitalize="none" placeholder="Type" accessibilityLabel="Desktop keyboard"
          style={[styles.textInput, compactLandscape && styles.textInputCompact, { color: colors.textPrimary, borderColor: colors.borderSubtle }]} />
      )}
      {tool("return-down-back-outline", "Enter", () => sendNamedKey("Enter"))}
      {tool("arrow-back-outline", "Backspace", () => sendNamedKey("Backspace"))}
      {tool("chevron-down-outline", "Hide keyboard", () => { Keyboard.dismiss(); setKeyboard(false); })}
    </View> : null}
    {inputNotice ? <Text accessibilityRole="alert" style={styles.inputNotice}>{inputNotice}</Text> : null}
    </KeyboardAvoidingView>
  </SafeAreaView>;
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  header: { paddingHorizontal: 16, paddingVertical: 10, gap: 4 },
  // Short landscape heights keep the header on one compact line so the video
  // viewport and the toolbar stay reachable.
  headerLandscape: { flexDirection: "row", alignItems: "baseline", gap: 12, paddingHorizontal: 12, paddingVertical: 4 },
  hostLandscape: { flexShrink: 1 },
  host: { fontSize: 14, fontWeight: "600" },
  viewport: { flex: 1, backgroundColor: "#000000", overflow: "hidden" },
  empty: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 16 },
  message: { fontSize: 15, color: "#e3e7eb", textAlign: "center" },
  action: { backgroundColor: "#edf1f4", paddingHorizontal: 20, paddingVertical: 12, borderRadius: 6 },
  actionText: { color: "#182024", fontSize: 15, fontWeight: "600" },
  toolbar: { flexDirection: "row", flexWrap: "wrap", justifyContent: "space-evenly", borderTopWidth: StyleSheet.hairlineWidth, padding: 4 },
  toolbarBreak: { width: "100%" },
  tool: { width: 44, height: 44, alignItems: "center", justifyContent: "center", borderRadius: 6 },
  keyboard: { flexDirection: "row", padding: 8, alignItems: "center" },
  // Landscape + keyboard open: the docked IME provides the keys (including its
  // own hide control), so the native capture field is clipped to one pixel
  // while staying mounted and focused. The remote video keeps the height above
  // the toolbar and the IME instead of a thin strip.
  keyboardCompact: { height: 1, paddingVertical: 0, paddingHorizontal: 0, overflow: "hidden" },
  textInputCompact: { height: 1, borderWidth: 0, paddingHorizontal: 0 },
  inputNotice: { paddingHorizontal: 12, paddingBottom: 6, fontSize: 12, color: "#e3ba76" },
  textInput: { flex: 1, minWidth: 0, height: 44, borderWidth: 1, borderRadius: 6, paddingHorizontal: 12 },
});
