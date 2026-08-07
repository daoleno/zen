import React, {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useReducer,
  useRef,
} from 'react';
import {
  ActivityIndicator,
  Platform,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from 'react-native';
import { Asset } from 'expo-asset';
import { Ionicons } from '@expo/vector-icons';
import { WebView } from 'react-native-webview';
import {
  buildTerminalChrome,
} from '../../constants/terminalThemes';
import { Typography } from '../../constants/tokens';
import { buildGhosttyTerminalHtml } from './ghosttyWebViewHtml';
import { TerminalInputHandler } from './TerminalInputHandler';
import type { TerminalSurfaceHandle, TerminalSurfaceProps } from './TerminalSurface.types';
import {
  TERMINAL_RENDERER_READY_TIMEOUT_MS,
  createTerminalRendererBootstrapState,
  reduceTerminalRendererBootstrap,
  terminalFontCache,
} from './terminalSurfaceBootstrap';
import { terminalWebViewBaseUrl } from './terminalWebViewSource';
import { terminalWebViewDensityProps } from './terminalFontDensity';
import { useGhosttyTerminalController } from './useGhosttyTerminalController';

export const TerminalSurfaceGhosttyWebView = forwardRef<
  TerminalSurfaceHandle,
  TerminalSurfaceProps
>(({
  serverId,
  targetId,
  backend = 'tmux',
  theme,
  ctrlArmed = false,
  onCtrlArmedChange,
  skillsHandoffToken,
  onSkillsHandoffFailure,
}, ref) => {
  const chrome = useMemo(
    () => buildTerminalChrome(theme),
    [theme],
  );
  const initialThemeRef = useRef(theme);
  const font = useMemo(() => terminalFontCache.resolve(
    () => Asset.fromModule(require('../../assets/fonts/MapleMono-CN-Regular.ttf')),
    (message, error) => {
      if (error === undefined) {
        console.warn('[Terminal font] ' + message);
      } else {
        console.warn('[Terminal font] ' + message, error);
      }
    },
  ), []);
  const [renderer, dispatchRenderer] = useReducer(
    reduceTerminalRendererBootstrap,
    undefined,
    createTerminalRendererBootstrapState,
  );
  const reportRendererFailure = useCallback((
    message: string,
    generation: number,
  ) => {
    const normalized = message.trim() || 'Terminal renderer failed before it became ready.';
    console.error('[Terminal renderer] ' + normalized);
    dispatchRenderer({
      type: 'failure',
      generation,
      message: normalized,
    });
  }, []);

  const controller = useGhosttyTerminalController({
    serverId,
    targetId,
    backend,
    theme,
    rendererGeneration: renderer.generation,
    onCtrlArmedChange,
    onRendererBootstrapFailure: reportRendererFailure,
    skillsHandoffToken,
    onSkillsHandoffFailure,
  });

  const html = useMemo(
    () => buildGhosttyTerminalHtml(
      initialThemeRef.current,
      font.uri,
      Typography.terminalSize,
      renderer.generation,
    ),
    [font.uri, renderer.generation],
  );
  const baseUrl = useMemo(
    () => (font.uri
      ? terminalWebViewBaseUrl(font.uri, Platform.OS)
      : 'https://zen.local/'),
    [font.uri],
  );

  useEffect(() => {
    if (renderer.status !== 'loading') {
      return;
    }
    const generation = renderer.generation;
    const timer = setTimeout(() => {
      const message = `Terminal renderer did not become ready within ${TERMINAL_RENDERER_READY_TIMEOUT_MS}ms.`;
      console.error('[Terminal renderer] ' + message);
      dispatchRenderer({ type: 'timeout', generation, message });
    }, TERMINAL_RENDERER_READY_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [renderer.generation, renderer.status]);

  useEffect(() => {
    if (controller.readyGeneration === null) {
      return;
    }
    dispatchRenderer({
      type: 'ready',
      generation: controller.readyGeneration,
    });
  }, [controller.readyGeneration]);

  const handleRendererLoadStart = useCallback(() => {
    controller.onRendererLoadStart(renderer.generation);
    dispatchRenderer({
      type: 'load-start',
      generation: renderer.generation,
    });
  }, [controller, renderer.generation]);

  const handleRendererRetry = useCallback(() => {
    dispatchRenderer({ type: 'retry' });
  }, []);

  useImperativeHandle(ref, () => ({
    sendInput: controller.sendInput,
    focus: controller.focus,
    blur: controller.blur,
    wakeRenderer: controller.wakeRenderer,
    resumeInput: controller.resumeInput,
    scrollToBottom: controller.scrollToBottom,
  }), [controller]);

  return (
    <View
      collapsable={false}
      style={[styles.container, { backgroundColor: theme.background }]}
    >
      <WebView
        key={`terminal-renderer:${renderer.generation}`}
        ref={controller.webviewRef}
        originWhitelist={['*']}
        source={{ html, baseUrl }}
        onLoadStart={handleRendererLoadStart}
        onMessage={controller.onRendererMessage}
        onError={(event) => {
          reportRendererFailure(
            event.nativeEvent.description || 'Terminal WebView navigation failed.',
            renderer.generation,
          );
        }}
        onHttpError={(event) => {
          reportRendererFailure(
            `Terminal WebView HTTP ${event.nativeEvent.statusCode}: ${event.nativeEvent.description}`,
            renderer.generation,
          );
        }}
        onContentProcessDidTerminate={() => {
          reportRendererFailure(
            'Terminal WebView content process terminated.',
            renderer.generation,
          );
        }}
        onRenderProcessGone={(event) => {
          reportRendererFailure(
            event.nativeEvent.didCrash
              ? 'Terminal WebView render process crashed.'
              : 'Terminal WebView render process exited.',
            renderer.generation,
          );
        }}
        javaScriptEnabled
        domStorageEnabled
        allowFileAccess
        textInteractionEnabled
        scrollEnabled={false}
        bounces={false}
        automaticallyAdjustContentInsets={false}
        contentInsetAdjustmentBehavior="never"
        allowsLinkPreview={false}
        overScrollMode="never"
        {...terminalWebViewDensityProps(Platform.OS)}
        // Keep a dedicated compositor layer so focus transitions do not leave
        // a stale blank buffer until the next touch.
        androidLayerType="hardware"
        style={[styles.webview, { backgroundColor: theme.background }]}
      />

      {renderer.status === 'loading' && !controller.ready ? (
        <View style={[styles.loading, { backgroundColor: theme.background }]}>
          <ActivityIndicator color={theme.cursor} />
        </View>
      ) : null}

      {renderer.status === 'failed' ? (
        <View
          accessibilityRole="alert"
          style={[styles.failure, { backgroundColor: theme.background }]}
        >
          <Text style={[styles.failureTitle, { color: theme.foreground }]}>
            Terminal renderer could not start
          </Text>
          <Text style={[styles.failureDetail, { color: chrome.textMuted }]}>
            {renderer.error || 'The embedded terminal did not become ready.'}
          </Text>
          <TouchableOpacity
            accessibilityRole="button"
            accessibilityLabel="Retry Terminal renderer"
            activeOpacity={0.72}
            onPress={handleRendererRetry}
            style={[
              styles.retryButton,
              {
                backgroundColor: chrome.surfaceActive,
                borderColor: chrome.borderStrong,
              },
            ]}
          >
            <Text style={[styles.retryText, { color: chrome.text }]}>Retry</Text>
          </TouchableOpacity>
        </View>
      ) : null}

      {controller.scrolledUp && controller.ready && (
        <TouchableOpacity
          accessibilityLabel="Scroll to bottom"
          accessibilityRole="button"
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
    position: 'absolute',
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    alignItems: 'center',
    justifyContent: 'center',
  },
  failure: {
    position: 'absolute',
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: 28,
  },
  failureTitle: {
    fontSize: 15,
    fontWeight: '600',
    textAlign: 'center',
  },
  failureDetail: {
    marginTop: 8,
    fontSize: 12,
    lineHeight: 18,
    textAlign: 'center',
  },
  retryButton: {
    marginTop: 18,
    minWidth: 96,
    minHeight: 40,
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: 1,
    borderRadius: 999,
    paddingHorizontal: 18,
  },
  retryText: {
    fontSize: 13,
    fontWeight: '600',
  },
  jumpButton: {
    position: 'absolute',
    right: 16,
    bottom: 16,
    borderWidth: 1,
    borderRadius: 999,
    width: 44,
    height: 44,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
