// Explicit test entry only; never imported by runtime routes.
import React, { useState } from "react";
import { registerRootComponent } from "expo";
import { Button, Text, View } from "react-native";
import { NativeDesktopView } from "../modules/zen-remote-desktop/src";
import { startPinnedTunnel, stopPinnedTunnel } from "../modules/zen-link-transport/src";

function Harness() {
  const [connection, setConnection] = useState("");
  const [command, setCommand] = useState("");
  const [status, setStatus] = useState("Disconnected");
  const send = (value: object) => setCommand(JSON.stringify({ ...value, sequence: Date.now() }));
  const connect = async () => {
    try {
      await stopPinnedTunnel("desktop-owned-test").catch(() => undefined);
      const config = await (await fetch("http://127.0.0.1:18089/config")).json();
      const tunnel = await startPinnedTunnel("desktop-owned-test", "10.0.2.2", Number(config.port), config.pin, "on-demand");
      setConnection(JSON.stringify({ url: `ws://127.0.0.1:${tunnel.port}/desktop`, authorization: config.authorization }));
    } catch (error) { setStatus(String(error)); }
  };
  return <View style={{ flex: 1, paddingTop: 40, backgroundColor: "white" }}>
    <Text style={{ color: "black", padding: 12 }}>Zen native desktop verification</Text>
    <Text testID="desktop-status" style={{ color: "black", padding: 12 }}>{status}</Text>
    <NativeDesktopView style={{ flex: 1 }} connection={connection} command={command} onState={({ nativeEvent }) => {
      setStatus(JSON.stringify(nativeEvent));
    }} />
    <Button title="Connect" onPress={connect} />
    <Button title="Request control" onPress={() => send({ type: "start", source: "x11", control: true })} />
    <Button title="Mouse key scroll" onPress={() => send({ type: "batch", events: [
      { type: "pointer", x: 0.5, y: 0.6 }, { type: "button", code: 1, down: true }, { type: "button", code: 1, down: false },
      { type: "key", code: 97, down: true }, { type: "key", code: 97, down: false }, { type: "scroll", delta: 3 },
    ] })} />
    <Button title="Stop" onPress={() => setConnection("")} />
  </View>;
}

registerRootComponent(Harness);
