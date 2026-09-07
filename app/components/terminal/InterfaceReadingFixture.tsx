import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  Image,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useAppTheme } from "../../constants/tokens";
import { buildChatChrome } from "../../theme";
import type { CodexConversationEvent } from "../../services/codexConversation";
import { InterfaceChatTimelineSection } from "./InterfaceChatTimelineSection";
import { InterfaceChatKeyboardFrame } from "./InterfaceChatKeyboardFrame";
import { usePinnedTimeline } from "./InterfaceChatSurfaceHooks";
import type { SessionFilePreviewLoader } from "./SessionFilePreviewSheet";
import { readingFixtureMessage as message } from "./interfaceReadingFixtureData";

const noop = () => {};
const images: Record<string, number> = {
  "large.jpg": require("../../assets/reading-fixture/large-jpeg.jpg"),
  "normal.png": require("../../assets/reading-fixture/normal.png"),
  "large.png": require("../../assets/reading-fixture/large.png"),
  "tall.png": require("../../assets/reading-fixture/tall.png"),
};
const loader: SessionFilePreviewLoader = {
  metadata: async (_server, request) => {
    console.info("READING_FIXTURE image request", request.path);
    return {
      name: request.path,
      path: request.path,
      relativePath: request.path,
      kind: "image",
      contentType: request.path.endsWith(".jpg") ? "image/jpeg" : "image/png",
      size: 1000,
      modifiedAt: "2026-09-08T00:00:00Z",
      generation: "fixture-generation",
      tooLarge: false,
      previewLimitBytes: 16000000,
    };
  },
  binary: async (_server, _daemon, request) => ({
    uri: Image.resolveAssetSource(images[request.path]).uri,
    headers: {},
  }),
  text: async () => {
    throw new Error("Not a fixture text file");
  },
};
const initial = Array.from({ length: 81 }, (_, index) =>
  message(
    index,
    index % 10 === 9
      ? `## Images ${index}\n\n[JPEG image](large.jpg)\n\n[Normal image](normal.png)\n\n[Large image](large.png)\n\n[Tall image](tall.png)`
      : undefined,
  ),
);

// Explicit dev-only route, isolated data and files; production hooks/renderer.
// No scenario command scrolls the list: gestures and the shared policy own it.
export function InterfaceReadingFixture() {
  const { theme: appTheme } = useAppTheme();
  const { chrome, theme } = useMemo(
    () => buildChatChrome(appTheme),
    [appTheme],
  );
  const [events, setEvents] = useState(initial);
  const [streaming, setStreaming] = useState(false);
  const [away, setAway] = useState(false);
  const [generation, setGeneration] = useState(0);
  const [conversation, setConversation] = useState("A");
  useEffect(() => {
    console.info("READING_FIXTURE mounted");
    return () => console.info("READING_FIXTURE unmounted");
  }, []);
  useEffect(() => {
    if (!streaming) return;
    let arrivals = 0;
    const timer = setInterval(() => {
      console.info("READING_ARRIVAL", ++arrivals);
      setEvents((current) => {
        const last = current.at(-1)!;
        return [
          ...current.slice(0, -1),
          {
            ...last,
            body: last.body + "\n\nStream arrival: 中文内容继续增长。",
            partial: true,
          },
        ];
      });
    }, 500);
    return () => clearInterval(timer);
  }, [streaming]);
  return (
    <SafeAreaView
      style={[styles.root, { backgroundColor: chrome.appBackground }]}
    >
      <View style={styles.tools}>
        {[
          [
            "Append",
            () =>
              setEvents((current) => [
                ...current,
                message((current.at(-1)?.seq ?? -1) + 1),
              ]),
          ],
          [streaming ? "Stop stream" : "Stream", () => setStreaming((v) => !v)],
          [
            "Older",
            () =>
              setEvents((current) => [
                ...Array.from({ length: 10 }, (_, i) =>
                  message((current[0].seq ?? 0) - 10 + i),
                ),
                ...current,
              ]),
          ],
          [away ? "Return" : "Navigate", () => setAway((v) => !v)],
          ["Reconnect", () => setGeneration((v) => v + 1)],
          [conversation, () => setConversation((v) => (v === "A" ? "B" : "A"))],
        ].map(([label, action]) => (
          <Pressable
            key={String(label)}
            accessibilityRole="button"
            onPress={action as () => void}
            style={styles.button}
          >
            <Text style={{ color: chrome.text, fontSize: 12 }}>
              {String(label)}
            </Text>
          </Pressable>
        ))}
      </View>
      {away ? (
        <View style={styles.root} />
      ) : (
        <ReadingSurface
          key={`${conversation}:${generation}`}
          conversation={conversation}
          events={events}
          chrome={chrome}
          theme={theme}
        />
      )}
    </SafeAreaView>
  );
}
function ReadingSurface({
  conversation,
  events,
  chrome,
  theme,
}: {
  conversation: string;
  events: CodexConversationEvent[];
  chrome: ReturnType<typeof buildChatChrome>["chrome"];
  theme: ReturnType<typeof buildChatChrome>["theme"];
}) {
  const [loaded, setLoaded] = useState(false);
  useEffect(() => setLoaded(true), []);
  const pinned = usePinnedTimeline(
    loaded ? events.length : 0,
    `reading-fixture:${conversation}`,
    0,
  );
  const [draft, setDraft] = useState("");
  const [focused, setFocused] = useState(false);
  const input = useRef<TextInput>(null);
  return (
    <InterfaceChatKeyboardFrame
      enabled
      composerFocused={focused}
      keyboardVerticalOffset={0}
      chrome={chrome}
      onKeyboardLifecycleInvalidate={(reason) => {
        if (reason !== "app") input.current?.blur();
        if (reason !== "ime_closed") pinned.clearTurnFocusForLifecycle();
      }}
      composer={
        <View
          style={[styles.composer, { backgroundColor: chrome.appBackground }]}
        >
          <TextInput
            ref={input}
            accessibilityLabel="Fixture composer"
            value={draft}
            onChangeText={setDraft}
            onFocus={() => setFocused(true)}
            onBlur={() => setFocused(false)}
            style={{ color: chrome.text, flex: 1, minWidth: 0 }}
          />
          <Pressable
            accessibilityRole="button"
            onPress={() =>
              console.info(
                "READING_SAMPLE",
                JSON.stringify(pinned.readingPosition.current()),
              )
            }
          >
            <Text style={{ color: chrome.text }}>Measure</Text>
          </Pressable>
          <Pressable
            accessibilityRole="button"
            onPress={() => pinned.scrollToLatest()}
          >
            <Text style={{ color: chrome.text }}>Latest</Text>
          </Pressable>
        </View>
      }
      renderTimeline={(clearance, gate) => (
        <InterfaceChatTimelineSection
          serverId="reading-fixture"
          serverUrl="http://fixture.invalid"
          daemonId="fixture"
          workerId="fixture-session"
          workerProcessId={1}
          workerStartedAt={1}
          conversation={null}
          events={loaded ? events : []}
          pendingUserMessages={[]}
          loading={!loaded}
          commandMenuOpen={false}
          scrollRef={pinned.scrollRef}
          readingPosition={pinned.readingPosition}
          textSelectable={pinned.textSelectable}
          extraContentPadding={clearance}
          keyboardLifecycleGate={gate}
          turnFocusClearanceRequest={pinned.turnFocusClearanceRequest}
          turnFocusSpacer={pinned.turnFocusSpacer}
          turnFocusPendingMessageId={pinned.turnFocusPendingMessageId}
          topChromeInset={0}
          chrome={chrome}
          theme={theme}
          onLayout={pinned.handleLayout}
          onScroll={pinned.handleScroll}
          onScrollBeginDrag={pinned.handleScrollBeginDrag}
          onScrollEndDrag={pinned.handleScrollEndDrag}
          onMomentumScrollBegin={pinned.handleMomentumScrollBegin}
          onMomentumScrollEnd={pinned.handleMomentumScrollEnd}
          onTouchActiveChange={pinned.handleTimelineTouchActiveChange}
          onItemsMutated={pinned.handleTimelineItemsMutated}
          onContentSizeChange={pinned.handleContentSizeChange}
          onClearanceChange={pinned.handleClearanceChange}
          onTurnFocusAnchorAvailable={pinned.handleTurnFocusAnchorAvailable}
          onTurnFocusRowLayout={pinned.handleTurnFocusRowLayout}
          onTurnFocusSpacerLayout={pinned.handleTurnFocusSpacerLayout}
          onTextSelectionGestureStart={pinned.handleTextSelectionGestureStart}
          onTextSelectionGestureEnd={pinned.handleTextSelectionGestureEnd}
          onRetryPendingUserMessage={noop}
          filePreviewLoader={loader}
        />
      )}
    />
  );
}
const styles = StyleSheet.create({
  root: { flex: 1 },
  tools: { flexDirection: "row", flexWrap: "wrap", height: 80 },
  button: {
    width: "33.333%",
    height: 40,
    padding: 8,
    justifyContent: "center",
  },
  composer: { flexDirection: "row", minHeight: 52, padding: 12, gap: 12 },
});
