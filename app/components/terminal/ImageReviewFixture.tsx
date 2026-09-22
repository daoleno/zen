import React, { useEffect, useMemo, useRef, useState } from "react";
import { Image, Pressable, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { AttachmentUploadQueue } from "../../services/attachmentUploadQueue";
import { pickUploadDocuments } from "../../services/uploads";
import type { ComposerAttachment } from "./InterfaceChatSession";
import { InterfaceComposerAttachmentRail } from "./InterfaceComposerAttachmentRail";
import { ZenImage, ZenImageOwnerContext } from "./ZenImage";

/** Inert native QA: production chooser, queue, attachment rail and image viewer;
 * uploaded bytes never leave the emulator and no provider is called. */
export function ImageReviewFixture({ chrome }: { chrome: TerminalThemeChrome }) {
  const [owner, setOwner] = useState(0);
  const [attachments, setAttachments] = useState<ComposerAttachment[]>([]);
  const [error, setError] = useState("");
  const queue = useRef<AttachmentUploadQueue | null>(null);
  const generation = useRef(0);
  const imageOwner = useMemo(() => ({ key: `image-review:${owner}`, async resolve(path: string) {
    return { uri: Image.resolveAssetSource(path === "tall.png" ? require("../../assets/reading-fixture/tall.png") : require("../../assets/reading-fixture/normal.png")).uri, headers: {} };
  } }), [owner]);
  useEffect(() => {
    generation.current++;
    const attempts = new Map<string, number>();
    const current = new AttachmentUploadQueue(async (asset, progress) => {
      const attempt = (attempts.get(asset.uri) ?? 0) + 1; attempts.set(asset.uri, attempt);
      let rejectOperation: (error: Error) => void = () => {};
      const result = new Promise<{ name: string; path: string; localUri: string; mimeType?: string }>((resolve, reject) => {
        rejectOperation = reject;
        setTimeout(() => { progress({ transferredBytes: 1, totalBytes: 2, fraction: 0.5 }); if (attempt === 1) reject(new Error("Fixture transfer failed · retry this file")); else resolve({ name: asset.name, path: asset.name, localUri: asset.uri, mimeType: asset.mimeType }); }, 500);
      });
      return { result, cancel() { rejectOperation(new Error("Fixture cancelled")); return null; } };
    }, (items) => setAttachments(items.map((item) => ({ id: item.id, name: item.asset.name, path: item.result?.path || "", localUri: item.asset.uri, mimeType: item.asset.mimeType, uploadStatus: item.status, uploadError: item.error, uploadProgress: item.progress, retryUpload: () => current.retry(item.id) }))));
    queue.current = current; setAttachments([]);
    return () => { generation.current++; current.dispose(); queue.current = null; };
  }, [owner]);
  async function select() {
    const epoch = generation.current;
    try { const assets = await pickUploadDocuments(8 - attachments.length); if (generation.current === epoch) queue.current?.enqueue(assets.map((asset, index) => ({ id: `${epoch}:${Date.now()}:${index}`, asset }))); }
    catch (error) { if (generation.current === epoch) setError(error instanceof Error ? error.message : "Selection failed"); }
  }
  const images = [{ kind: "owned" as const, path: "normal.png", name: "Normal fixture" }, { kind: "owned" as const, path: "tall.png", name: "Tall fixture" }];
  return <ZenImageOwnerContext.Provider value={imageOwner}><View style={{ padding: 12, gap: 10 }}>
    <Text style={{ color: chrome.text }}>Image/upload fixture · Owner {owner}</Text>
    <View style={{ flexDirection: "row", gap: 16 }}><Pressable accessibilityRole="button" onPress={() => void select()} style={{ padding: 12 }}><Text style={{ color: chrome.accent }}>Select files</Text></Pressable><Pressable accessibilityRole="button" onPress={() => setOwner((value) => value + 1)} style={{ padding: 12 }}><Text style={{ color: chrome.accent }}>Switch owner</Text></Pressable></View>
    {error ? <Text style={{ color: chrome.danger }}>{error}</Text> : null}
    <InterfaceComposerAttachmentRail attachments={attachments} activeUpload={null} chrome={chrome} onRemoveAttachment={(id) => queue.current?.remove(id)} onCancelUpload={() => { for (const item of attachments) queue.current?.remove(item.id); }} />
    {images.map((source) => <ZenImage key={source.path} source={source} gallery={images} chrome={chrome} />)}
  </View></ZenImageOwnerContext.Provider>;
}
