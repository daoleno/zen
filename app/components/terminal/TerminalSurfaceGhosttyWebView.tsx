import React, { forwardRef, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, StyleSheet, TouchableOpacity, View } from 'react-native';
import { Asset } from 'expo-asset';
import { Ionicons } from '@expo/vector-icons';
import { WebView } from 'react-native-webview';
import {
  buildTerminalChrome,
  DefaultTerminalThemeName,
  resolveTerminalTheme,
} from '../../constants/terminalThemes';
import { buildGhosttyTerminalHtml } from './ghosttyWebViewHtml';
import { TerminalInputHandler } from './TerminalInputHandler';
import type { TerminalSurfaceHandle, TerminalSurfaceProps } from './TerminalSurface.types';
import { useGhosttyTerminalController } from './useGhosttyTerminalController';

let cachedTerminalFontUri: string | null = null;

export const TerminalSurfaceGhosttyWebView = forwardRef<
  TerminalSurfaceHandle,
  TerminalSurfaceProps
>(({
  serverId,
  targetId,
  backend = 'tmux',
  themeName = DefaultTerminalThemeName,
  themeOverrides,
  ctrlArmed = false,
  onCtrlArmedChange,
}, ref) => {
  const [fontUri, setFontUri] = useState<string | null>(cachedTerminalFontUri);
  const theme = useMemo(
    () => resolveTerminalTheme(themeName, themeOverrides),
    [themeName, themeOverrides],
  );
  const chrome = useMemo(
    () => buildTerminalChrome(theme),
    [theme],
  );
  const initialThemeRef = useRef(theme);

  const controller = useGhosttyTerminalController({
    serverId,
    targetId,
    backend,
    theme,
    onCtrlArmedChange,
  });

  const html = useMemo(
    () => (fontUri ? buildGhosttyTerminalHtml(initialThemeRef.current, fontUri) : ''),
    [fontUri],
  );

  useEffect(() => {
    if (cachedTerminalFontUri) {
      setFontUri(cachedTerminalFontUri);
      return;
    }

    let cancelled = false;

    (async () => {
      const asset = Asset.fromModule(require('../../assets/fonts/MapleMono-CN-Regular.ttf'));
      if (!asset.localUri) {
        await asset.downloadAsync();
      }
      if (!cancelled) {
        cachedTerminalFontUri = asset.localUri ?? asset.uri;
        setFontUri(cachedTerminalFontUri);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, []);

  useImperativeHandle(ref, () => ({
    sendInput: controller.sendInput,
    focus: controller.focus,
    blur: controller.blur,
    resumeInput: controller.resumeInput,
    scrollToBottom: controller.scrollToBottom,
  }), [controller]);

  return (
    <View style={[styles.container, { backgroundColor: theme.background }]}>
      {fontUri ? (
        <WebView
          ref={controller.webviewRef}
          originWhitelist={['*']}
          source={{ html, baseUrl: 'https://zen.local/' }}
          onLoadStart={controller.onRendererLoadStart}
          onMessage={controller.onRendererMessage}
          javaScriptEnabled
          domStorageEnabled
          allowFileAccess
          textInteractionEnabled
          scrollEnabled={false}
          bounces={false}
          overScrollMode="never"
          style={[styles.webview, { backgroundColor: theme.background }]}
        />
      ) : null}

      {(!fontUri || !controller.ready) && (
        <View style={[styles.loading, { backgroundColor: theme.background }]}>
          <ActivityIndicator color={theme.cursor} />
        </View>
      )}

      {controller.scrolledUp && controller.ready && (
        <TouchableOpacity
          style={[
            styles.jumpButton,
            {
              backgroundColor: chrome.overlay,
              borderColor: chrome.borderStrong,
            },
          ]}
          onPress={controller.scrollToBottom}
          activeOpacity={0.7}
        >
          <Ionicons name="arrow-down" size={16} color={chrome.text} />
        </TouchableOpacity>
      )}

      <TerminalInputHandler
        ref={controller.inputRef}
        onInput={controller.onInput}
        ctrlArmed={ctrlArmed}
        onCtrlConsumed={controller.onCtrlConsumed}
      />
    </View>
  );
});

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  webview: {
    flex: 1,
  },
  loading: {
    ...StyleSheet.absoluteFillObject,
    alignItems: 'center',
    justifyContent: 'center',
  },
  jumpButton: {
    position: 'absolute',
    right: 16,
    bottom: 16,
    borderWidth: 1,
    borderRadius: 999,
    width: 38,
    height: 38,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
