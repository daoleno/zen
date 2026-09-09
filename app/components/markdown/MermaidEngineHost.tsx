import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { StyleSheet, View } from "react-native";
import { WebView, type WebViewMessageEvent } from "react-native-webview";
import { MERMAID_MAX_RENDER_MS, MERMAID_MESSAGE_VERSION } from "./mermaidLimits";
import {
  isAllowedMermaidEngineUrl,
  parseMermaidHostMessage,
  serializeMermaidRenderRequest,
} from "./mermaidMessages";
import type { MermaidRenderTheme } from "./mermaidTheme";
import {
  MERMAID_ENGINE_BASE_URL,
  buildMermaidEngineHtml,
} from "./mermaidWebViewHtml";

export type MermaidEngineResult =
  | { ok: true; svg: string; width: number; height: number }
  | { ok: false; error: string };

type PendingRender = {
  requestId: string;
  generation: number;
  source: string;
  theme: MermaidRenderTheme;
  timeoutMs: number;
  onResult: (result: MermaidEngineResult) => void;
};

const pending = new Map<string, PendingRender>();
let queue: PendingRender[] = [];
let engineReady = false;
let inject: ((script: string) => void) | null = null;
let nextRequest = 1;

function flushQueue() {
  if (!engineReady || !inject) {
    return;
  }
  while (queue.length > 0) {
    const job = queue.shift();
    if (!job || !pending.has(job.requestId)) {
      continue;
    }
    inject(
      `window.__zenMermaidRender(${serializeMermaidRenderRequest({
        v: MERMAID_MESSAGE_VERSION,
        type: "render",
        requestId: job.requestId,
        generation: job.generation,
        source: job.source,
        theme: job.theme,
        timeoutMs: job.timeoutMs,
      })}); true;`,
    );
  }
}

export function requestMermaidEngineRender(input: {
  source: string;
  theme: MermaidRenderTheme;
  generation: number;
  timeoutMs?: number;
  onResult: (result: MermaidEngineResult) => void;
}) {
  const requestId = `m${nextRequest}`;
  nextRequest += 1;
  const job: PendingRender = {
    requestId,
    generation: input.generation,
    source: input.source,
    theme: input.theme,
    timeoutMs: input.timeoutMs ?? MERMAID_MAX_RENDER_MS,
    onResult: input.onResult,
  };
  pending.set(requestId, job);
  queue.push(job);
  flushQueue();
  return () => {
    pending.delete(requestId);
  };
}

export function MermaidEngineHost() {
  const webviewRef = useRef<WebView>(null);
  const html = useMemo(() => buildMermaidEngineHtml(), []);
  const handleMessage = useCallback((event: WebViewMessageEvent) => {
    const message = parseMermaidHostMessage(event.nativeEvent.data);
    if (!message) {
      return;
    }
    if (message.type === "ready") {
      engineReady = true;
      flushQueue();
      return;
    }
    const job = pending.get(message.requestId);
    if (!job || job.generation !== message.generation) {
      return;
    }
    pending.delete(message.requestId);
    if (message.ok) {
      job.onResult({
        ok: true,
        svg: message.svg,
        width: message.width,
        height: message.height,
      });
      return;
    }
    job.onResult({ ok: false, error: message.error });
  }, []);

  useEffect(() => {
    inject = (script: string) => {
      webviewRef.current?.injectJavaScript(script);
    };
    return () => {
      if (inject) {
        engineReady = false;
        inject = null;
      }
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
          engineReady = true;
          flushQueue();
        }}
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
        dataDetectorTypes="none"
        style={styles.webview}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  host: {
    position: "absolute",
    width: 8,
    height: 8,
    opacity: 0.01,
    overflow: "hidden",
  },
  webview: {
    width: 8,
    height: 8,
    backgroundColor: "transparent",
    opacity: 0.01,
  },
});
