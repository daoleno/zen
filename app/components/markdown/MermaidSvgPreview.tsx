import React, { useEffect, useMemo, useState } from "react";
import { StyleSheet, View } from "react-native";
import { WebView } from "react-native-webview";
import { isAllowedMermaidEngineUrl } from "./mermaidMessages";
import {
  acquireMermaidPreviewSlot,
  releaseMermaidPreviewSlot,
} from "./mermaidPreviewSlots";
import { MERMAID_ENGINE_BASE_URL, buildMermaidSvgPreviewHtml } from "./mermaidWebViewHtml";

export function MermaidSvgPreview({
  svg,
  width,
  height,
  background,
  priority = "inline",
}: {
  svg: string;
  width: number;
  height: number;
  background: string;
  priority?: "inline" | "fullscreen";
}) {
  const html = useMemo(
    () => buildMermaidSvgPreviewHtml(svg, background),
    [background, svg],
  );
  const [held, setHeld] = useState(priority === "fullscreen");

  useEffect(() => {
    if (priority === "fullscreen") {
      setHeld(true);
      return;
    }
    const acquired = acquireMermaidPreviewSlot();
    setHeld(acquired);
    if (!acquired) {
      return;
    }
    return () => {
      releaseMermaidPreviewSlot();
    };
  }, [priority]);

  return (
    <View style={[styles.frame, { width, height }]} pointerEvents="none">
      {held ? (
        <WebView
          originWhitelist={["https://zen.local"]}
          source={{ html, baseUrl: `${MERMAID_ENGINE_BASE_URL}-svg` }}
          onShouldStartLoadWithRequest={(request) =>
            isAllowedMermaidEngineUrl(request.url)
          }
          setSupportMultipleWindows={false}
          javaScriptCanOpenWindowsAutomatically={false}
          javaScriptEnabled={false}
          domStorageEnabled={false}
          thirdPartyCookiesEnabled={false}
          sharedCookiesEnabled={false}
          cacheEnabled={false}
          allowFileAccess={false}
          allowFileAccessFromFileURLs={false}
          allowUniversalAccessFromFileURLs={false}
          mixedContentMode="never"
          incognito
          scrollEnabled={false}
          bounces={false}
          scalesPageToFit={false}
          showsHorizontalScrollIndicator={false}
          showsVerticalScrollIndicator={false}
          webviewDebuggingEnabled={false}
          nestedScrollEnabled={false}
          overScrollMode="never"
          allowsLinkPreview={false}
          dataDetectorTypes={["none"]}
          style={[styles.webview, { width, height }]}
        />
      ) : (
        <View
          accessibilityLabel="Diagram preview deferred"
          style={[styles.webview, { width, height }]}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  frame: {
    overflow: "hidden",
    backgroundColor: "transparent",
  },
  webview: {
    backgroundColor: "transparent",
  },
});
