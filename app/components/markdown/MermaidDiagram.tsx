import { Ionicons } from "@expo/vector-icons";
import * as Clipboard from "expo-clipboard";
import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Pressable,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { withAlpha } from "../terminal/colorWithAlpha";
import {
  createCodeBlockCopyFeedback,
} from "../terminal/InterfaceMessageCodeBlockCopy";
import { mermaidCacheKey, readMermaidCache, writeMermaidCache } from "./mermaidCache";
import { mermaidFailureLabel, prepareMermaidDiagram } from "./mermaidEngine";
import { requestMermaidEngineRender } from "./MermaidEngineHost";
import { MermaidFullscreenModal } from "./MermaidFullscreenModal";
import { MERMAID_INLINE_MAX_HEIGHT } from "./mermaidLimits";
import { mermaidPreviewLayout, parseMermaidSvgSize } from "./mermaidSvgLayout";
import {
  mermaidThemeFromChrome,
  mermaidThemeIsDark,
} from "./mermaidTheme";
import { MermaidSvgPreview } from "./MermaidSvgPreview";

interface MermaidDiagramProps {
  source: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
  isLast?: boolean;
  streaming?: boolean;
  closed?: boolean;
  fallback: React.ReactNode;
}

export function MermaidDiagram({
  source,
  chrome,
  theme,
  compact = false,
  isLast = false,
  streaming = false,
  closed = true,
  fallback,
}: MermaidDiagramProps) {
  const { width: windowWidth } = useWindowDimensions();
  const [copied, setCopied] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [containerWidth, setContainerWidth] = useState(Math.max(1, windowWidth - 48));
  const [render, setRender] = useState<{
    svg: string;
    width: number;
    height: number;
  } | null>(null);
  const [renderError, setRenderError] = useState<string | null>(null);
  const generationRef = useRef(0);
  const prepared = useMemo(
    () => prepareMermaidDiagram(source, { streaming, closed }),
    [closed, source, streaming],
  );
  const mermaidTheme = useMemo(
    () =>
      mermaidThemeFromChrome(
        chrome,
        theme,
        mermaidThemeIsDark(theme) ? "dark" : "light",
      ),
    [chrome, theme],
  );
  const copyFeedback = useMemo(
    () =>
      createCodeBlockCopyFeedback({
        copyText: Clipboard.setStringAsync,
        onCopiedChange: setCopied,
        scheduleReset: setTimeout,
        cancelReset: clearTimeout,
      }),
    [],
  );

  useEffect(() => {
    return () => copyFeedback.dispose();
  }, [copyFeedback]);

  useEffect(() => {
    generationRef.current += 1;
    const generation = generationRef.current;
    setRenderError(null);
    if (!prepared.ok) {
      setRender(null);
      return;
    }
    const cacheKey = mermaidCacheKey(prepared.source, mermaidTheme);
    const cached = readMermaidCache(cacheKey);
    if (cached) {
      setRender(cached);
      return;
    }
    setRender(null);
    const cancel = requestMermaidEngineRender({
      source: prepared.source,
      theme: mermaidTheme,
      generation,
      onResult: (result) => {
        if (generation !== generationRef.current) {
          return;
        }
        if (!result.ok) {
          setRenderError(result.error);
          return;
        }
        const size = parseMermaidSvgSize(result.svg, {
          width: result.width,
          height: result.height,
        });
        const next = { svg: result.svg, ...size };
        writeMermaidCache(cacheKey, next);
        setRender(next);
      },
    });
    return cancel;
  }, [mermaidTheme, prepared]);

  const layout = useMemo(() => {
    if (!render) {
      return {
        width: containerWidth,
        height: compact ? 96 : 112,
        overflow: false,
        scale: 1,
      };
    }
    return mermaidPreviewLayout(
      render,
      containerWidth,
      compact ? 220 : MERMAID_INLINE_MAX_HEIGHT,
    );
  }, [compact, containerWidth, render]);

  const copySource = useCallback(() => {
    void copyFeedback.copy(source);
  }, [copyFeedback, source]);

  const showFallback =
    Boolean(renderError) ||
    (!prepared.ok && prepared.reason !== "incomplete");
  const failureLabel = renderError
    ? "Couldn't render diagram"
    : !prepared.ok
      ? mermaidFailureLabel(prepared.reason)
      : null;

  if (showFallback) {
    return (
      <View style={isLast ? styles.blockLast : null}>
        {failureLabel && failureLabel !== "Diagram" ? (
          <Text style={[styles.fallbackHint, { color: chrome.textMuted }]}>
            {failureLabel}
          </Text>
        ) : null}
        {fallback}
      </View>
    );
  }

  return (
    <View
      style={[
        styles.frame,
        compact ? styles.frameCompact : null,
        {
          backgroundColor:
            compact || theme.background === "transparent"
              ? chrome.surface
              : withAlpha(theme.foreground, 0.04),
          borderColor: chrome.border,
        },
        isLast ? styles.blockLast : null,
      ]}
      onLayout={(event) => {
        const width = Math.floor(event.nativeEvent.layout.width);
        if (width > 0 && Math.abs(width - containerWidth) > 1) {
          setContainerWidth(width);
        }
      }}
    >
      <View
        style={[
          styles.header,
          {
            borderBottomColor: chrome.border,
            backgroundColor: withAlpha(theme.foreground, compact ? 0.035 : 0.045),
          },
        ]}
      >
        <Text numberOfLines={1} style={[styles.label, { color: chrome.textMuted }]}>
          Diagram
        </Text>
        <View style={styles.actions}>
          {render ? (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel="Expand diagram"
              hitSlop={8}
              onPress={() => setExpanded(true)}
              style={styles.iconButton}
            >
              <Ionicons name="expand-outline" size={16} color={chrome.textMuted} />
            </Pressable>
          ) : null}
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={copied ? "Diagram source copied" : "Copy diagram source"}
            hitSlop={8}
            onPress={copySource}
            style={styles.iconButton}
          >
            <Ionicons
              name={copied ? "checkmark" : "copy-outline"}
              size={16}
              color={copied ? chrome.accent : chrome.textMuted}
            />
          </Pressable>
        </View>
      </View>
      {render ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Diagram preview"
          onPress={() => setExpanded(true)}
          style={[styles.preview, { height: layout.height }]}
        >
          <MermaidSvgPreview
            svg={render.svg}
            width={layout.width}
            height={layout.height}
            background="transparent"
          />
        </Pressable>
      ) : (
        <View
          accessibilityLabel="Diagram"
          style={[styles.placeholder, { height: layout.height }]}
        />
      )}
      {render ? (
        <MermaidFullscreenModal
          visible={expanded}
          svg={render.svg}
          width={render.width}
          height={render.height}
          chrome={chrome}
          onClose={() => setExpanded(false)}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  frame: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
    maxWidth: "100%",
    marginTop: 2,
    marginBottom: 10,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  frameCompact: {
    marginBottom: 8,
  },
  header: {
    borderBottomWidth: StyleSheet.hairlineWidth,
    minHeight: 34,
    paddingLeft: 10,
    paddingRight: 3,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
  },
  label: {
    flex: 1,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.chatMonoFontBold,
  },
  actions: {
    flexDirection: "row",
    alignItems: "center",
  },
  iconButton: {
    width: 34,
    height: 34,
    alignItems: "center",
    justifyContent: "center",
  },
  preview: {
    width: "100%",
    overflow: "hidden",
  },
  placeholder: {
    width: "100%",
    minHeight: 72,
  },
  fallbackHint: {
    marginBottom: 6,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.chatFont,
  },
  blockLast: {
    marginBottom: 0,
  },
});
