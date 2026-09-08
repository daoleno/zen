// Explicit test entry only; never imported by runtime routes.
import React, { useEffect, useRef, useState } from "react";
import { registerRootComponent } from "expo";
import { AppState, Button, Text, View } from "react-native";
import { NativeDesktopView } from "../modules/zen-remote-desktop/src";
import { startPinnedTunnel, stopPinnedTunnel } from "../modules/zen-link-transport/src";
import { DesktopCommandQueue, type DesktopCommandTarget } from "../services/remoteDesktopCommands";

function Harness() {
  const [connection, setConnection] = useState("");
  const [status, setStatus] = useState("Disconnected");
  const native = useRef<DesktopCommandTarget>(null);
  const [commands] = useState(() => new DesktopCommandQueue(() => native.current, (reason) => {
    setConnection(""); setStatus(reason);
  }));
  const inputGeneration = commands.currentGeneration;
  const send = (value: object) => commands.send(value);
  const stop = () => { commands.stop(); setConnection(""); };
  useEffect(() => {
    const subscription = AppState.addEventListener("change", (state) => {
      if (state !== "active") { commands.stop(); setConnection(""); }
    });
    return () => { commands.stop(); subscription.remove(); };
  }, [commands]);
  const connect = async () => {
    const inputGeneration = commands.begin();
    setConnection("");
    try {
      await stopPinnedTunnel("desktop-owned-test").catch(() => undefined);
      const config = await (await fetch("http://127.0.0.1:18089/config")).json();
      const tunnel = await startPinnedTunnel("desktop-owned-test", "10.0.2.2", Number(config.port), config.pin, "on-demand");
      if (inputGeneration === commands.currentGeneration) {
        setConnection(JSON.stringify({ url: `ws://127.0.0.1:${tunnel.port}/desktop`, authorization: config.authorization, inputGeneration }));
      }
    } catch (error) {
      if (inputGeneration === commands.currentGeneration) { commands.stop(); setStatus(String(error)); }
    }
  };
  return <View style={{ flex: 1, paddingTop: 40, backgroundColor: "white" }}>
    <Text style={{ color: "black", padding: 12 }}>Zen native desktop verification</Text>
    <Text testID="desktop-status" style={{ color: "black", padding: 12 }}>{status}</Text>
    <NativeDesktopView ref={native} key={connection} style={{ flex: 1 }} connection={connection} onState={({ nativeEvent }) => {
      if (inputGeneration !== commands.currentGeneration) return;
      if (["disconnected", "denied", "unsupported"].includes(nativeEvent.state)) stop();
      setStatus(JSON.stringify(nativeEvent));
    }} />
    <Button title="Connect" onPress={connect} />
    <Button title="Request control" onPress={() => send({ type: "start", source: "x11", control: true })} />
    <Button title="Mouse key scroll" onPress={() => send({ type: "batch", events: [
      { type: "pointer", x: 0.5, y: 0.6 }, { type: "button", code: 1, down: true }, { type: "button", code: 1, down: false },
      { type: "key", code: 97, down: true }, { type: "key", code: 97, down: false }, { type: "scroll", delta: 3 },
    ] })} />
    <Button title="Stop" onPress={stop} />
  </View>;
}

registerRootComponent(Harness);
