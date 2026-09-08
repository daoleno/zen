import React, { useCallback, useEffect, useRef, useState } from "react";
import { AppState, PanResponder, Pressable, StyleSheet, Switch, Text, TextInput, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Stack, useFocusEffect } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { useCurrentServer } from "../store/currentServer";
import { useAppColors } from "../constants/tokens";
import { prepareDesktopConnection } from "../services/remoteDesktop";
import { desktopKey, desktopPoint, desktopText, desktopStart, type DesktopInput } from "../services/remoteDesktopModel";
import { NativeDesktopView, type DesktopState } from "../modules/zen-remote-desktop/src";
import { DesktopCommandQueue, type DesktopCommandTarget } from "../services/remoteDesktopCommands";

export default function RemoteDesktopScreen() {
  const { currentServer } = useCurrentServer();
  return <DesktopSession key={currentServer?.id ?? "none"} />;
}

function DesktopSession() {
  const { currentServer } = useCurrentServer();
  const colors = useAppColors();
  const [connection, setConnection] = useState("");
  const [status, setStatus] = useState<DesktopState>({ state: "disconnected" });
  const [preparing, setPreparing] = useState(false);
  const [control, setControl] = useState(false);
  const [panMode, setPanMode] = useState(false);
  const [dragMode, setDragMode] = useState(false);
  const [keyboard, setKeyboard] = useState(false);
  const [text, setText] = useState("");
  const [zoom, setZoom] = useState(1);
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const [size, setSize] = useState({ width: 1, height: 1 });
  const generation = useRef(0);
  const native = useRef<DesktopCommandTarget>(null);
  const [commands] = useState(() => new DesktopCommandQueue(() => native.current, (reason) => {
    generation.current++;
    setConnection(""); setPreparing(false); setControl(false); setKeyboard(false);
    setStatus({ state: "disconnected", reason });
  }));
  const inputGeneration = commands.currentGeneration;
  const gestureStart = useRef({ x: 0, y: 0, time: 0, pinch: 0, zoom: 1, offset: { x: 0, y: 0 } });
  const connected = status.state === "connected";
  const send = (value: object) => commands.send(value);
  const input = (events: DesktopInput[]) => {
    if (connected && control && events.length) send({ type: "batch", events });
  };
  const stop = useCallback(() => {
    generation.current++;
    commands.stop();
    setConnection(""); setPreparing(false); setControl(false);
    setStatus({ state: "disconnected" }); setKeyboard(false);
    setDragMode(false); setPanMode(false); setZoom(1); setOffset({ x: 0, y: 0 });
  }, [commands]);
  useFocusEffect(useCallback(() => stop, [stop]));
  useEffect(() => {
    const subscription = AppState.addEventListener("change", (state) => { if (state !== "active") stop(); });
    return () => { generation.current++; commands.stop(); subscription.remove(); };
  }, [commands, stop]);
  const connect = async () => {
    if (!currentServer) return;
    const epoch = ++generation.current;
    const inputGeneration = commands.begin();
    setConnection(""); setControl(false);
    setPreparing(true); setStatus({ state: "disconnected" });
    try {
      const next = await prepareDesktopConnection(currentServer, inputGeneration);
      if (epoch === generation.current) setConnection(next);
    } catch (error) {
      if (epoch === generation.current) {
        commands.stop();
        setStatus({ state: "disconnected", reason: error instanceof Error ? error.message : "Connection failed." });
      }
    } finally { if (epoch === generation.current) setPreparing(false); }
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
        zoom, offset };
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
        setOffset({ x: gestureStart.current.offset.x + gesture.dx, y: gestureStart.current.offset.y + gesture.dy });
      } else {
        const point = pointer(event.nativeEvent.locationX, event.nativeEvent.locationY);
        if (point) input([{ type: "pointer", ...point }]);
      }
    },
    onPanResponderRelease: (event, gesture) => {
      if (dragMode) input([{ type: "release" }]);
      if (!dragMode && !panMode && !gestureStart.current.pinch && Math.hypot(gesture.dx, gesture.dy) < 10) {
        const point = pointer(event.nativeEvent.locationX, event.nativeEvent.locationY);
        const code = Date.now() - gestureStart.current.time > 550 ? 3 : 1;
        if (point) input([{ type: "pointer", ...point }, { type: "button", code, down: true }, { type: "button", code, down: false }]);
      }
    },
    onPanResponderTerminate: () => input([{ type: "release" }]),
  });
  const tool = (icon: React.ComponentProps<typeof Ionicons>["name"], label: string, action: () => void, selected = false, disabled = false) => (
    <Pressable key={label} accessibilityRole="button" accessibilityLabel={label} accessibilityState={{ selected, disabled }}
      disabled={disabled} onPress={action} style={[styles.tool, { backgroundColor: selected ? colors.surfacePressed : "transparent", opacity: disabled ? 0.35 : 1 }]}>
      <Ionicons name={icon} size={22} color={colors.textPrimary} />
    </Pressable>
  );
  return <SafeAreaView edges={["bottom"]} style={[styles.root, { backgroundColor: colors.surfaceSubtle }]}>
    <Stack.Screen options={{ title: "Remote Desktop", headerShown: true }} />
    <View style={styles.header}>
      <Text numberOfLines={1} style={[styles.host, { color: colors.textPrimary }]}>{currentServer?.name ?? "No current server"}</Text>
      <Text style={{ color: colors.textSecondary }}>{preparing ? "Connecting" : status.state === "streaming" ? "Waiting for video" : status.state === "requesting" ? "Awaiting permission" : connected ? "Connected" : ""}</Text>
    </View>
    <View style={styles.viewport} onLayout={(event) => setSize(event.nativeEvent.layout)}>
      {connection ? <View style={[StyleSheet.absoluteFill, { transform: [{ translateX: offset.x }, { translateY: offset.y }, { scale: zoom }] }]}>
        <NativeDesktopView ref={native} key={connection} style={styles.root} connection={connection} onState={({ nativeEvent }) => {
          if (commands.currentGeneration !== inputGeneration) return;
          if (["disconnected", "denied", "unsupported"].includes(nativeEvent.state)) stop();
          setStatus(nativeEvent);
        }} />
      </View> : null}
      {connected ? <View style={StyleSheet.absoluteFill} {...responder.panHandlers} /> : <View style={styles.empty}>
        <Ionicons name="desktop-outline" size={40} color="#b9bec5" />
        <Text style={styles.message}>{status.reason || (status.state === "sources" ? status.source === "wayland" ? "Wayland portal desktop" : "Selected X11 desktop" : status.state === "requesting" ? "Awaiting host permission" : status.state === "streaming" ? "Waiting for video" : preparing ? "Connecting" : "Disconnected")}</Text>
        {status.state === "sources" ? <>
          <View style={styles.mode}><Text style={styles.message}>Allow control</Text><Switch value={control} onValueChange={setControl} /></View>
          <Pressable accessibilityRole="button" disabled={!desktopStart(status.source, control)} onPress={() => {
            const start = desktopStart(status.source, control);
            if (start) send(start);
          }} style={styles.action}><Text style={styles.actionText}>Share desktop</Text></Pressable>
        </> : !preparing && !["requesting", "streaming"].includes(status.state) ? <Pressable accessibilityRole="button" disabled={!currentServer} onPress={connect} style={styles.action}><Text style={styles.actionText}>Connect</Text></Pressable> : null}
      </View>}
    </View>
    <View style={[styles.toolbar, { borderColor: colors.borderSubtle }]}>
      {tool("stop-circle-outline", "Stop desktop", stop, false, !connection)}
      {tool("hand-left-outline", "Pan desktop", () => { setPanMode(!panMode); setDragMode(false); }, panMode, !connected)}
      {tool("move-outline", "Drag pointer", () => { setDragMode(!dragMode); setPanMode(false); }, dragMode, !connected || !control)}
      {tool("contract-outline", "Reset zoom", () => { setZoom(1); setOffset({ x: 0, y: 0 }); }, false, !connected)}
      {tool("keypad-outline", "Keyboard", () => setKeyboard(!keyboard), keyboard, !connected || !control)}
      {tool("chevron-up-outline", "Scroll up", () => input([{ type: "scroll", delta: -3 }]), false, !connected || !control)}
      {tool("chevron-down-outline", "Scroll down", () => input([{ type: "scroll", delta: 3 }]), false, !connected || !control)}
    </View>
    {keyboard ? <View style={styles.keyboard}>
      <TextInput autoFocus value={text} onChangeText={(value) => { input(desktopText(value)); setText(""); }}
        onKeyPress={({ nativeEvent }) => { if (nativeEvent.key === "Backspace") input(desktopKey(0xff08)); }}
        onSubmitEditing={() => input(desktopKey(0xff0d))} autoCorrect={false} autoCapitalize="none" placeholder="Type" accessibilityLabel="Desktop keyboard"
        style={[styles.textInput, { color: colors.textPrimary, borderColor: colors.borderSubtle }]} />
      {tool("return-down-back-outline", "Enter", () => input(desktopKey(0xff0d)))}
      {tool("arrow-back-outline", "Backspace", () => input(desktopKey(0xff08)))}
    </View> : null}
  </SafeAreaView>;
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  header: { paddingHorizontal: 16, paddingVertical: 10, gap: 4 },
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
