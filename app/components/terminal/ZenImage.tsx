import React, { createContext, useContext, useEffect, useMemo, useState } from "react";
import { ActivityIndicator, Image, Modal, Pressable, StyleSheet, Text, View } from "react-native";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { SafeAreaProvider, SafeAreaView } from "react-native-safe-area-context";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { imageSourceKey, resolveImageSource, type ZenImageOwner, type ZenImageSource } from "../../services/imageSource";
import type { SessionFileBinarySource } from "../../services/sessionFilePreview";
import { SessionFileImagePreview } from "./SessionFileImagePreview";

export const ZenImageOwnerContext = createContext<ZenImageOwner | null>(null);

function useImage(source: ZenImageSource, owner: ZenImageOwner | null, attempt: number) {
  const key = useMemo(() => imageSourceKey(source, owner?.key || "phone"), [source, owner?.key]);
  const [state, setState] = useState<{ key: string; source?: SessionFileBinarySource; error?: string }>({ key });
  useEffect(() => {
    const controller = new AbortController();
    setState({ key });
    void resolveImageSource(source, owner, controller.signal).then(
      (resolved) => { if (!controller.signal.aborted) setState({ key, source: resolved }); },
      (error: unknown) => { if (!controller.signal.aborted) setState({ key, error: error instanceof Error ? error.message : "Image unavailable" }); },
    );
    return () => controller.abort();
    // The identity includes the canonical owner; object identity need not reload images.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, attempt, owner]);
  return state.key === key ? state : { key };
}

export function ZenImage({ source, chrome, gallery, compact = false }: {
  source: ZenImageSource; chrome: TerminalThemeChrome; gallery?: ZenImageSource[]; compact?: boolean;
}) {
  const owner = useContext(ZenImageOwnerContext);
  const identity = useMemo(() => imageSourceKey(source, owner?.key || "phone"), [source, owner?.key]);
  const [opened, setOpened] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [failed, setFailed] = useState<string | null>(null);
  const [loaded, setLoaded] = useState<string | null>(null);
  const [ratio, setRatio] = useState(1.5);
  const resolved = useImage(source, owner, attempt);
  const error = resolved.error || (failed === identity ? "Could not load image" : null);
  const images = useMemo(() => gallery?.length ? gallery : [source], [gallery, source]);
  const initial = useMemo(() => Math.max(0, images.findIndex((image) => imageSourceKey(image, owner?.key || "phone") === identity)), [images, owner?.key, identity]);
  const retry = () => { setFailed(null); setLoaded(null); setAttempt((value) => value + 1); };
  return (
    <View style={{ width: compact ? 96 : "100%", maxWidth: 400 }}>
      <Pressable accessibilityRole="button" accessibilityLabel={error ? `Retry ${source.name}` : `Open ${source.name}`} onPress={error ? retry : () => setOpened(identity)}
        style={[styles.preview, { borderColor: chrome.border, backgroundColor: chrome.surfaceMuted, height: compact ? 80 : Math.max(100, Math.min(280, 320 / ratio)) }]}>
        {resolved.source && !error ? <Image key={`${identity}:${attempt}`} source={resolved.source} resizeMode="contain" resizeMethod="resize"
          style={StyleSheet.absoluteFill} accessibilityLabel={source.name}
          onLoad={(event) => { setLoaded(identity); const { width, height } = event.nativeEvent.source; if (width > 0 && height > 0) setRatio(width / height); }}
          onError={() => setFailed(identity)} /> : null}
        {error ? <><Ionicons name="refresh-outline" size={22} color={chrome.textMuted} /><Text numberOfLines={2} style={{ color: chrome.textMuted }}>{error}</Text></> : loaded !== identity ? <ActivityIndicator color={chrome.textMuted} /> : null}
        <View style={styles.expand}><Ionicons name="expand-outline" size={17} color={chrome.text} /></View>
      </Pressable>
      {opened === identity ? <ImageGallery key={identity} images={images} initial={initial} chrome={chrome} onClose={() => setOpened(null)} /> : null}
    </View>
  );
}

function ImageGallery({ images, initial, chrome, onClose }: { images: ZenImageSource[]; initial: number; chrome: TerminalThemeChrome; onClose(): void }) {
  const owner = useContext(ZenImageOwnerContext);
  const [index, setIndex] = useState(initial);
  const [attempt, setAttempt] = useState(0);
  const [failed, setFailed] = useState(false);
  const source = images[Math.min(index, images.length - 1)];
  const resolved = useImage(source, owner, attempt);
  const error = resolved.error || (failed ? "Could not load image" : null);
  const move = (next: number) => { setFailed(false); setIndex(next); };
  return <Modal visible animationType="fade" onRequestClose={onClose} presentationStyle="fullScreen">
    <GestureHandlerRootView style={{ flex: 1 }}><SafeAreaProvider><SafeAreaView style={{ flex: 1, backgroundColor: chrome.surfaceMuted }}>
      <View style={styles.toolbar}>
        <Text numberOfLines={1} style={{ flex: 1, color: chrome.text }}>{source.name}</Text>
        <Pressable accessibilityRole="button" accessibilityLabel="Close image" onPress={onClose} style={styles.button}><Ionicons name="close-outline" size={26} color={chrome.text} /></Pressable>
      </View>
      {error ? <Pressable accessibilityRole="button" accessibilityLabel="Retry image" onPress={() => { setFailed(false); setAttempt((value) => value + 1); }} style={styles.state}><Ionicons name="refresh-outline" size={28} color={chrome.text} /><Text style={{ color: chrome.text }}>{error}</Text></Pressable>
        : resolved.source ? <SessionFileImagePreview key={`${resolved.key}:${attempt}`} source={resolved.source} chrome={chrome} onError={() => setFailed(true)} />
        : <View style={styles.state}><ActivityIndicator color={chrome.text} /></View>}
      {images.length > 1 ? <View style={styles.toolbar}>
        <Pressable accessibilityRole="button" accessibilityLabel="Previous image" disabled={index === 0} onPress={() => move(index - 1)} style={styles.button}><Ionicons name="chevron-back" size={24} color={index === 0 ? chrome.textSubtle : chrome.text} /></Pressable>
        <Text style={{ color: chrome.text }}>{index + 1} / {images.length}</Text>
        <Pressable accessibilityRole="button" accessibilityLabel="Next image" disabled={index === images.length - 1} onPress={() => move(index + 1)} style={styles.button}><Ionicons name="chevron-forward" size={24} color={chrome.text} /></Pressable>
      </View> : null}
    </SafeAreaView></SafeAreaProvider></GestureHandlerRootView>
  </Modal>;
}

const styles = StyleSheet.create({
  preview: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 10, overflow: "hidden", alignItems: "center", justifyContent: "center", padding: 8 },
  expand: { position: "absolute", right: 6, bottom: 6 },
  toolbar: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingHorizontal: 12 },
  button: { padding: 12, minWidth: 48, minHeight: 48, alignItems: "center" },
  state: { flex: 1, alignItems: "center", justifyContent: "center", gap: 12 },
});
