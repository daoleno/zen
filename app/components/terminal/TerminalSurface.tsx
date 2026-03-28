import React, { forwardRef, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import { WebView, WebViewMessageEvent } from 'react-native-webview';
import { Colors } from '../../constants/tokens';
import {
  DefaultTerminalThemeName,
  resolveTerminalTheme,
  TerminalThemeName,
  TerminalThemePalette,
} from '../../constants/terminalThemes';
import { xtermCss, xtermFitAddonJs, xtermJs } from './xtermAssets';
import { useTerminalSession } from './useTerminalSession';

type BridgeMessage =
  | { type: 'ready' }
  | { type: 'input'; data: string }
  | { type: 'resize'; cols: number; rows: number }
  | { type: 'scroll'; lines: number };

type NativeToTerminalMessage =
  | { type: 'output'; data: string }
  | { type: 'theme'; theme: TerminalThemePalette }
  | { type: 'session'; cols: number; rows: number };

export interface TerminalSurfaceHandle {
  sendInput(data: string): void;
  focus(): void;
  scrollToBottom(): void;
}

export const TerminalSurface = forwardRef<TerminalSurfaceHandle, {
  targetId: string;
  backend?: string;
  themeName?: TerminalThemeName;
  themeOverrides?: Partial<TerminalThemePalette>;
}>(({
  targetId,
  backend = 'tmux',
  themeName = DefaultTerminalThemeName,
  themeOverrides,
}, ref) => {
  const webviewRef = useRef<WebView>(null);
  const pendingRef = useRef<NativeToTerminalMessage[]>([]);
  const initialSizeRef = useRef<{ cols: number; rows: number } | null>(null);
  const [ready, setReady] = useState(false);
  const theme = useMemo(() => resolveTerminalTheme(themeName, themeOverrides), [themeName, themeOverrides]);

  const html = useMemo(() => buildTerminalHtml(theme), [theme]);

  const postToTerminal = (payload: NativeToTerminalMessage) => {
    if (!ready) {
      pendingRef.current.push(payload);
      return;
    }
    injectTerminalMessage(payload);
  };

  const injectTerminalMessage = (payload: NativeToTerminalMessage) => {
    let script = '';
    if (payload.type === 'output') {
      script = `window.__zenOutput && window.__zenOutput(${JSON.stringify(payload.data)}); true;`;
    } else if (payload.type === 'theme') {
      script = `window.__zenTheme && window.__zenTheme(${JSON.stringify(payload.theme)}); true;`;
    } else if (payload.type === 'session') {
      script = `window.__zenSession && window.__zenSession(${JSON.stringify({ cols: payload.cols, rows: payload.rows })}); true;`;
    }
    if (script) {
      webviewRef.current?.injectJavaScript(script);
    }
  };

  const focusTerminal = () => {
    webviewRef.current?.injectJavaScript('window.__zenFocus && window.__zenFocus(); true;');
  };

  const scrollToBottom = () => {
    webviewRef.current?.injectJavaScript('window.__zenScrollToBottom && window.__zenScrollToBottom(); true;');
  };

  useEffect(() => {
    if (!ready || pendingRef.current.length === 0) return;
    for (const message of pendingRef.current) {
      injectTerminalMessage(message);
    }
    pendingRef.current = [];
  }, [ready]);

  useEffect(() => {
    postToTerminal({ type: 'theme', theme });
  }, [theme]);

  const session = useTerminalSession(targetId, backend, {
    onOpen: ({ cols, rows }) => {
      postToTerminal({ type: 'session', cols, rows });
    },
    onHistory: () => {
      // Intentionally ignored. tmux redraws the current visible screen
      // via the PTY when attach-session runs — that's the live output path
      // and renders perfectly at the correct terminal width.
      // capture-pane history is a rendered text snapshot at a potentially
      // different width, which causes format corruption in xterm.js.
    },
    onOutput: ({ data }) => {
      postToTerminal({ type: 'output', data });
    },
    onExit: ({ exit_code }) => {
      postToTerminal({
        type: 'output',
        data: `\r\n[zen] session exited with code ${exit_code}\r\n`,
      });
    },
    onError: ({ message }) => {
      postToTerminal({
        type: 'output',
        data: `\r\n[zen] ${message}\r\n`,
      });
    },
  });

  useImperativeHandle(ref, () => ({
    sendInput(data: string) {
      session.sendInput(data);
      focusTerminal();
      scrollToBottom();
    },
    focus() {
      focusTerminal();
    },
    scrollToBottom() {
      scrollToBottom();
    },
  }), [session]);

  const onMessage = (event: WebViewMessageEvent) => {
    try {
      const payload = JSON.parse(event.nativeEvent.data) as BridgeMessage;
      if (payload.type === 'ready') {
        setReady(true);
        if (initialSizeRef.current) {
          session.open(initialSizeRef.current.cols, initialSizeRef.current.rows);
        }
        return;
      }
      if (payload.type === 'input') {
        session.sendInput(payload.data);
        return;
      }
      if (payload.type === 'resize') {
        initialSizeRef.current = { cols: payload.cols, rows: payload.rows };
        if (!ready) return;
        session.resize(payload.cols, payload.rows);
        return;
      }
      if (payload.type === 'scroll') {
        session.scroll(payload.lines);
        return;
      }
    } catch {
      // Ignore malformed bridge messages
    }
  };

  return (
    <View style={styles.container}>
      <WebView
        ref={webviewRef}
        originWhitelist={['*']}
        source={{ html, baseUrl: 'https://zen.local/' }}
        onMessage={onMessage}
        javaScriptEnabled
        domStorageEnabled
        allowFileAccess
        scrollEnabled={false}
        bounces={false}
        overScrollMode="never"
        style={styles.webview}
      />
      {!ready && (
        <View style={styles.loading}>
          <ActivityIndicator color={Colors.accent} />
        </View>
      )}
    </View>
  );
});

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
  },
  webview: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
  },
  loading: {
    ...StyleSheet.absoluteFillObject,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: Colors.bgPrimary,
  },
});

function buildTerminalHtml(theme: TerminalThemePalette) {
  return String.raw`<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta
      name="viewport"
      content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no"
    />
    <style>
      ${xtermCss}
      html, body {
        margin: 0;
        padding: 0;
        height: 100%;
        width: 100%;
        background: ${theme.background};
        overflow: hidden;
        overscroll-behavior: none;
      }
      #terminal {
        height: 100%;
        width: 100%;
        box-sizing: border-box;
      }
      /* Hide xterm scrollbar — touch handler drives scrolling */
      .xterm-viewport {
        overflow-y: hidden !important;
      }
      .xterm-viewport::-webkit-scrollbar {
        display: none;
      }
    </style>
  </head>
  <body>
    <div id="terminal"></div>
    <script>${xtermJs}</script>
    <script>${xtermFitAddonJs}</script>
    <script>
      const FONT_SIZE = 14;
      const LINE_HEIGHT_RATIO = 1.25;
      const LINE_HEIGHT_PX = Math.ceil(FONT_SIZE * LINE_HEIGHT_RATIO);

      const terminal = new Terminal({
        convertEol: false,
        cursorBlink: true,
        allowTransparency: false,
        fontFamily: 'Menlo, Consolas, monospace',
        fontSize: FONT_SIZE,
        lineHeight: LINE_HEIGHT_RATIO,
        theme: ${JSON.stringify(theme)},
        scrollback: 5000
      });
      const fitAddon = new FitAddon.FitAddon();
      terminal.loadAddon(fitAddon);
      terminal.open(document.getElementById('terminal'));
      let writeQueue = [];
      let writeScheduled = false;
      let isWriting = false;
      let initialized = false;

      const send = (payload) => {
        try { window.ReactNativeWebView.postMessage(JSON.stringify(payload)); }
        catch(_) {}
      };
      const reportSize = () => {
        try {
          fitAddon.fit();
          send({ type: 'resize', cols: terminal.cols, rows: terminal.rows });
        } catch (_) {}
      };

      // ── Touch scroll engine ─────────────────────────────────
      //
      // All scrolling is delegated to the daemon via tmux copy-mode.
      // tmux is the ONLY entity that has scrollback — xterm.js never
      // accumulates it because tmux renders via cursor positioning.
      // Touch gestures are converted to line counts and sent as bridge
      // messages → React Native → WebSocket → daemon → tmux copy-mode.
      //
      (function initTouchScroll() {
        let scrolling = false;
        let startY = 0;
        let lastY = 0;
        let lastTime = 0;
        let velocity = 0;
        let scrollAccum = 0;
        let momentumRAF = null;

        const THRESHOLD = 5;
        const MAX_VELOCITY = 8;
        const MIN_MOMENTUM = 0.12;
        const FRICTION = 0.93;
        const STOP_V = 0.3;
        const MAX_FRAMES = 180;

        // Send scroll request to daemon (negative = up/older, positive = down/newer)
        const doScroll = (lines) => {
          if (lines === 0) return;
          send({ type: 'scroll', lines: lines });
        };

        const cancelMomentum = () => {
          if (momentumRAF) {
            cancelAnimationFrame(momentumRAF);
            momentumRAF = null;
          }
        };

        document.addEventListener('touchstart', (e) => {
          cancelMomentum();
          scrolling = false;
          scrollAccum = 0;
          velocity = 0;
          startY = lastY = e.touches[0].clientY;
          lastTime = performance.now();
        }, { capture: true, passive: true });

        document.addEventListener('touchmove', (e) => {
          const y = e.touches[0].clientY;
          if (!scrolling && Math.abs(startY - y) > THRESHOLD) {
            scrolling = true;
            scrollAccum = 0;
          }
          if (!scrolling) return;

          e.preventDefault();
          e.stopPropagation();

          const delta = lastY - y;
          const now = performance.now();
          const dt = now - lastTime;

          if (dt > 0 && isFinite(delta / dt)) {
            velocity = 0.6 * velocity + 0.4 * (delta / dt);
          }
          lastTime = now;
          lastY = y;

          scrollAccum += delta;
          const lines = Math.trunc(scrollAccum / LINE_HEIGHT_PX);
          if (lines !== 0) {
            doScroll(lines);
            scrollAccum -= lines * LINE_HEIGHT_PX;
          }
        }, { capture: true, passive: false });

        document.addEventListener('touchend', () => {
          if (!scrolling) return;
          scrolling = false;

          let v = Math.max(-MAX_VELOCITY, Math.min(MAX_VELOCITY, velocity));
          if (Math.abs(v) < MIN_MOMENTUM) {
            return;
          }

          // Momentum scrolling via tmux copy-mode
          let frameV = v * 16;
          let accum = 0;
          let frames = 0;

          const tick = () => {
            frames++;
            if (Math.abs(frameV) < STOP_V || frames > MAX_FRAMES) {
              momentumRAF = null;
              return;
            }
            accum += frameV;
            const lines = Math.trunc(accum / LINE_HEIGHT_PX);
            if (lines !== 0) {
              doScroll(lines);
              accum -= lines * LINE_HEIGHT_PX;
            }
            frameV *= FRICTION;
            momentumRAF = requestAnimationFrame(tick);
          };
          momentumRAF = requestAnimationFrame(tick);
        }, { capture: true, passive: true });

        document.addEventListener('touchcancel', () => {
          scrolling = false;
          cancelMomentum();
        }, { capture: true, passive: true });
      })();

      // ── Write queue (chunked output) ────────────────────────
      const flushWriteQueue = () => {
        if (isWriting) return;
        const chunk = writeQueue.shift();
        if (chunk === undefined) {
          writeScheduled = false;
          return;
        }
        writeScheduled = true;
        isWriting = true;
        terminal.write(chunk, () => {
          isWriting = false;
          requestAnimationFrame(flushWriteQueue);
        });
      };

      const enqueueOutput = (data) => {
        if (!data) return;
        const maxChunkSize = 4096;
        for (let i = 0; i < data.length; i += maxChunkSize) {
          writeQueue.push(data.slice(i, i + maxChunkSize));
        }
        if (!writeScheduled) requestAnimationFrame(flushWriteQueue);
      };

      // ── Bridge API ──────────────────────────────────────────
      window.__zenOutput = (data) => enqueueOutput(data);
      window.__zenFocus = () => terminal.focus();
      window.__zenScrollToBottom = () => {
        terminal.scrollToBottom();
      };
      window.__zenTheme = (t) => {
        terminal.options.theme = t;
        document.body.style.background = t.background;
        document.documentElement.style.background = t.background;
      };
      window.__zenSession = () => {
        if (!initialized) {
          initialized = true;
          setTimeout(reportSize, 0);
        }
      };

      terminal.onData((data) => send({ type: 'input', data }));
      window.addEventListener('resize', reportSize);

      setTimeout(() => {
        reportSize();
        send({ type: 'ready' });
      }, 0);
    </script>
  </body>
</html>`;
}
