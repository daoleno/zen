import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
} from "react";
import { StyleSheet, View } from "react-native";
import { WebView, type WebViewMessageEvent } from "react-native-webview";
import {
  bindMermaidEngineInject,
  completeMermaidEngineResult,
  failMermaidEngine,
  markMermaidEngineReady,
} from "./mermaidRenderQueue";
import {
  isAllowedMermaidEngineUrl,
  parseMermaidHostMessage,
} from "./mermaidMessages";
import {
  MERMAID_ENGINE_BASE_URL,
  buildMermaidEngineHtml,
} from "./mermaidWebViewHtml";

export {
  requestMermaidEngineRender,
  type MermaidEngineResult,
} from "./mermaidRenderQueue";

export function MermaidEngineHost() {
  const webviewRef = useRef<WebView>(null);
  const html = useMemo(() => buildMermaidEngineHtml(), []);
  const handleMessage = useCallback((event: WebViewMessageEvent) => {
    const message = parseMermaidHostMessage(event.nativeEvent.data);
    if (!message) {
      return;
    }
    if (message.type === "ready") {
      markMermaidEngineReady();
      return;
    }
    completeMermaidEngineResult(
      message.requestId,
      message.generation,
      message.ok
        ? {
            ok: true,
            svg: message.svg,
            width: message.width,
            height: message.height,
          }
        : { ok: false, error: message.error },
    );
  }, []);

  useEffect(() => {
    bindMermaidEngineInject((script: string) => {
      webviewRef.current?.injectJavaScript(script);
    });
    return () => {
      bindMermaidEngineInject(null);
    };
  }, []);

  return (
    <View
      pointerEvents="none"
      collapsable={false}
      style={styles.host}
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
    >
      <WebView
        ref={webviewRef}
        originWhitelist={["https://zen.local"]}
        source={{ html, baseUrl: MERMAID_ENGINE_BASE_URL }}
        onMessage={handleMessage}
        onLoadEnd={() => {
          webviewRef.current?.injectJavaScript(
            `(function(){if(window.mermaid&&window.__zenMermaidRender&&window.ReactNativeWebView){window.ReactNativeWebView.postMessage(JSON.stringify({v:1,type:"ready"}));}})(); true;`,
          );
        }}
        onRenderProcessGone={() => failMermaidEngine("engine")}
        onContentProcessDidTerminate={() => failMermaidEngine("engine")}
        onShouldStartLoadWithRequest={(request) =>
          isAllowedMermaidEngineUrl(request.url)
        }
        setSupportMultipleWindows={false}
        javaScriptCanOpenWindowsAutomatically={false}
        javaScriptEnabled
        domStorageEnabled={false}
        thirdPartyCookiesEnabled={false}
        sharedCookiesEnabled={false}
        cacheEnabled={false}
        allowFileAccess={false}
        allowFileAccessFromFileURLs={false}
        allowUniversalAccessFromFileURLs={false}
        mixedContentMode="never"
        incognito
        startInLoadingState={false}
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
        style={styles.webview}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  host: {
    position: "absolute",
    left: 0,
    top: 0,
    width: 48,
    height: 48,
    opacity: 1,
    overflow: "hidden",
    zIndex: -1,
  },
  webview: {
    width: 48,
    height: 48,
    backgroundColor: "transparent",
    opacity: 1,
  },
});
