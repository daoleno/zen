import React, { useCallback, useMemo, useState } from "react";
import { Dimensions, Platform, ScrollView, StyleSheet, View } from "react-native";
import { WebView } from "react-native-webview";
import { buildPreformattedBlockHtml } from "./preformattedBlockWebViewHtml";

interface PreformattedCodeWebViewProps {
  text: string;
  color: string;
  compact: boolean;
}

interface SizeMessage {
  type: "size";
  width: number;
  height: number;
}

function parseSizeMessage(raw: string): SizeMessage | null {
  try {
    const payload = JSON.parse(raw) as Partial<SizeMessage>;
    if (
      payload.type !== "size"
      || typeof payload.width !== "number"
      || typeof payload.height !== "number"
    ) {
      return null;
    }
    return {
      type: "size",
      width: payload.width,
      height: payload.height,
    };
  } catch {
    return null;
  }
}

export function PreformattedCodeWebView({
  text,
  color,
  compact,
}: PreformattedCodeWebViewProps) {
  const fontSize = compact ? 12 : 13;
  const lineHeight = compact ? 18 : 20;
  const paddingHorizontal = compact ? 10 : 12;
  const paddingVertical = compact ? 9 : 10;
  const fallbackWidth = Dimensions.get("window").width;
  const [size, setSize] = useState({
    width: fallbackWidth,
    height: lineHeight + paddingVertical * 2,
  });

  const html = useMemo(
    () =>
      buildPreformattedBlockHtml({
        text,
        color,
        fontSize,
        lineHeight,
        paddingHorizontal,
        paddingVertical,
      }),
    [color, compact, fontSize, lineHeight, paddingHorizontal, paddingVertical, text],
  );

  const onMessage = useCallback((event: { nativeEvent: { data: string } }) => {
    const message = parseSizeMessage(event.nativeEvent.data);
    if (!message || message.width <= 0 || message.height <= 0) {
      return;
    }
    setSize((current) => {
      if (
        Math.abs(current.width - message.width) < 1
        && Math.abs(current.height - message.height) < 1
      ) {
        return current;
      }
      return {
        width: message.width,
        height: message.height,
      };
    });
  }, []);

  return (
    <ScrollView
      horizontal
      nestedScrollEnabled
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={styles.scrollContent}
    >
      <View style={styles.content}>
        <WebView
          originWhitelist={["*"]}
          source={{ html, baseUrl: "https://zen.local/" }}
          onMessage={onMessage}
          javaScriptEnabled
          domStorageEnabled
          scrollEnabled={false}
          bounces={false}
          overScrollMode="never"
          textInteractionEnabled
          scalesPageToFit={false}
          showsHorizontalScrollIndicator={false}
          showsVerticalScrollIndicator={false}
          style={[styles.webview, { width: size.width, height: size.height }]}
          containerStyle={styles.webviewContainer}
          {...(Platform.OS === "ios" ? { opaque: false } : null)}
        />
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  scrollContent: {
    flexGrow: 1,
    alignItems: "flex-start",
  },
  content: {
    flexShrink: 0,
    alignSelf: "flex-start",
  },
  webviewContainer: {
    backgroundColor: "transparent",
  },
  webview: {
    backgroundColor: "transparent",
  },
});