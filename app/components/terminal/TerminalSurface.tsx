import React, { forwardRef, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { Asset } from 'expo-asset';
import { WebView, WebViewMessageEvent } from 'react-native-webview';
import { Ionicons } from '@expo/vector-icons';
import { Colors, Typography } from '../../constants/tokens';
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
  | { type: 'session'; cols: number; rows: number }
  | { type: 'scrollState'; atBottom: boolean };

export interface TerminalSurfaceHandle {
  sendInput(data: string): void;
  focus(): void;
  scrollToBottom(): void;
}

export const TerminalSurface = forwardRef<TerminalSurfaceHandle, {
  serverId: string;
  targetId: string;
  backend?: string;
  themeName?: TerminalThemeName;
  themeOverrides?: Partial<TerminalThemePalette>;
}>(({
  serverId,
  targetId,
  backend = 'tmux',
  themeName = DefaultTerminalThemeName,
  themeOverrides,
}, ref) => {
  const webviewRef = useRef<WebView>(null);
  const pendingRef = useRef<NativeToTerminalMessage[]>([]);
  const initialSizeRef = useRef<{ cols: number; rows: number } | null>(null);
  const [fontUri, setFontUri] = useState<string | null>(null);
  const [ready, setReady] = useState(false);
  const [scrolledUp, setScrolledUp] = useState(false);
  const theme = useMemo(() => resolveTerminalTheme(themeName, themeOverrides), [themeName, themeOverrides]);

  const html = useMemo(() => buildTerminalHtml(theme, fontUri), [theme, fontUri]);

  useEffect(() => {
    let cancelled = false;

    (async () => {
      const asset = Asset.fromModule(require('../../assets/fonts/MapleMono-CN-Regular.ttf'));
      if (!asset.localUri) {
        await asset.downloadAsync();
      }
      if (!cancelled) {
        setFontUri(asset.localUri ?? asset.uri);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, []);

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
    } else if (payload.type === 'scrollState') {
      script = `window.__zenScrollState && window.__zenScrollState(${JSON.stringify({ atBottom: payload.atBottom })}); true;`;
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

  useEffect(() => {
    postToTerminal({ type: 'scrollState', atBottom: !scrolledUp });
  }, [scrolledUp]);

  const session = useTerminalSession(serverId, targetId, backend, {
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
    onScrollState: ({ at_bottom }) => {
      setScrolledUp(!at_bottom);
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
      {scrolledUp && ready && (
        <TouchableOpacity
          style={styles.jumpButton}
          onPress={() => {
            session.cancelScroll();
            scrollToBottom();
          }}
          activeOpacity={0.7}
        >
          <Ionicons name="arrow-down" size={16} color="rgba(255,255,255,0.8)" />
        </TouchableOpacity>
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
  jumpButton: {
    position: 'absolute',
    right: 16,
    bottom: 16,
    backgroundColor: 'rgba(30,50,80,0.85)',
    borderWidth: 1,
    borderColor: 'rgba(91,157,255,0.3)',
    borderRadius: 999,
    width: 38,
    height: 38,
    alignItems: 'center',
    justifyContent: 'center',
  },
});

function buildTerminalHtml(theme: TerminalThemePalette, fontUri: string | null) {
  const fontFace = fontUri
    ? `
      @font-face {
        font-family: 'ZenTerm';
        src: url('${fontUri}') format('truetype');
        font-display: swap;
      }
    `
    : '';
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
      ${fontFace}
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
      .xterm .xterm-helpers {
        left: 0;
        right: 0;
        bottom: 0;
        overflow: hidden;
      }
      /*
       * IME composition overlay — opaque background prevents
       * underlying terminal text from bleeding through on multiline.
       * Position is managed by JS patch (drops to the next line on overflow).
       */
      .xterm .composition-view {
        background: ${theme.background} !important;
        color: ${theme.foreground} !important;
        border-bottom: 1px solid ${theme.cursor};
        pointer-events: none !important;
        white-space: pre-wrap !important;
        overflow-wrap: anywhere !important;
        word-break: break-word !important;
        overflow: visible !important;
        padding: 0 2px;
        box-sizing: border-box;
      }
      .xterm .composition-view.active {
        display: block !important;
        visibility: visible !important;
      }
      #selection-layer {
        display: none;
        position: fixed;
        inset: 0;
        z-index: 20;
        background: ${theme.background};
        color: ${theme.foreground};
        overflow: auto;
        padding: 14px 14px 22px;
        box-sizing: border-box;
        user-select: text;
        -webkit-user-select: text;
        overscroll-behavior: contain;
      }
      #selection-layer.active {
        display: block;
      }
      #selection-close {
        position: sticky;
        top: 0;
        margin-left: auto;
        margin-bottom: 12px;
        display: flex;
        align-items: center;
        justify-content: center;
        width: 36px;
        height: 36px;
        border: 0;
        border-radius: 18px;
        background: rgba(255,255,255,0.08);
        color: ${theme.foreground};
        font-size: 18px;
      }
      #selection-text {
        margin: 0;
        white-space: pre-wrap;
        overflow-wrap: break-word;
        word-break: break-word;
        user-select: text;
        -webkit-user-select: text;
        font-family: ${JSON.stringify(fontUri ? 'ZenTerm, monospace' : 'monospace')};
        font-size: ${Typography.terminalSize}px;
        line-height: ${Math.round(Typography.terminalSize * 1.6)}px;
      }
    </style>
  </head>
  <body>
    <div id="terminal"></div>
    <div id="selection-layer">
      <button id="selection-close" type="button" aria-label="Close selection">×</button>
      <pre id="selection-text"></pre>
    </div>
    <script>${xtermJs}</script>
    <script>${xtermFitAddonJs}</script>
    <script>
      const FONT_SIZE = 13;
      const LINE_HEIGHT_RATIO = 1.28;
      const LINE_HEIGHT_PX = Math.ceil(FONT_SIZE * LINE_HEIGHT_RATIO);

      const terminal = new Terminal({
        convertEol: false,
        cursorBlink: true,
        allowTransparency: false,
        fontFamily: ${JSON.stringify(fontUri ? 'ZenTerm, monospace' : 'monospace')},
        fontSize: FONT_SIZE,
        lineHeight: LINE_HEIGHT_RATIO,
        letterSpacing: 0,
        theme: ${JSON.stringify(theme)},
        scrollback: 5000
      });
      const fitAddon = new FitAddon.FitAddon();
      terminal.loadAddon(fitAddon);
      terminal.open(document.getElementById('terminal'));
      const patchCompositionLayout = () => {
        try {
          const core = terminal._core;
          const helper = core && core._compositionHelper;
          const compositionView = core && core._compositionView;
          const helperContainer = terminal.element && terminal.element.querySelector('.xterm-helpers');
          const textarea = terminal.textarea;

          if (!helper || !compositionView || !helperContainer || helper.__zenCompositionPatched) {
            return;
          }

          const originalUpdate = helper.updateCompositionElements.bind(helper);
          helper.updateCompositionElements = (dontRecurse) => {
            originalUpdate(dontRecurse);

            try {
              if (!helper.isComposing) return;

              const containerWidth = helperContainer.clientWidth || terminal.element.clientWidth || 0;
              const cursorLeft = Number.parseFloat(compositionView.style.left || '0');
              const cursorTop = Number.parseFloat(compositionView.style.top || '0');
              const cellHeight =
                core &&
                core._renderService &&
                core._renderService.dimensions &&
                core._renderService.dimensions.css &&
                core._renderService.dimensions.css.cell
                  ? core._renderService.dimensions.css.cell.height
                  : FONT_SIZE * LINE_HEIGHT_RATIO;

              // Allow composition to use full container width.
              // When the caret is near the right edge, promote the IME block
              // to the next visual line so wraps restart from column 0.
              compositionView.style.width = 'auto';
              compositionView.style.maxWidth = containerWidth + 'px';
              compositionView.style.height = 'auto';
              compositionView.style.minHeight = cellHeight + 'px';
              compositionView.style.lineHeight = cellHeight + 'px';
              compositionView.style.textIndent = '0px';

              const availableWidth = Math.max(24, containerWidth - cursorLeft);
              const naturalWidth = compositionView.scrollWidth;
              const wrapFromNextLine = cursorLeft > 0 && naturalWidth > availableWidth;

              if (wrapFromNextLine) {
                compositionView.style.left = '0px';
                compositionView.style.top = cursorTop + cellHeight + 'px';
                compositionView.style.width = containerWidth + 'px';
                compositionView.style.maxWidth = containerWidth + 'px';
              } else {
                compositionView.style.left = cursorLeft + 'px';
                compositionView.style.top = cursorTop + 'px';
              }

              if (textarea) {
                const textareaLeft = wrapFromNextLine ? 0 : cursorLeft;
                const textareaTop = wrapFromNextLine ? cursorTop + cellHeight : cursorTop;
                const textareaWidth = wrapFromNextLine ? containerWidth : availableWidth;

                textarea.style.left = textareaLeft + 'px';
                textarea.style.top = textareaTop + 'px';
                textarea.style.maxWidth = textareaWidth + 'px';
                textarea.style.width = Math.max(Math.min(naturalWidth, textareaWidth), 1) + 'px';
                textarea.style.height = Math.max(compositionView.offsetHeight, cellHeight) + 'px';
                textarea.style.lineHeight = cellHeight + 'px';
              }
            } catch (_) {}
          };

          helper.__zenCompositionPatched = true;
        } catch (_) {}
      };
      patchCompositionLayout();
      let writeQueue = [];
      let writeScheduled = false;
      let isWriting = false;
      let initialized = false;
      let selectionMode = false;
      let remoteAtBottom = true;
      const selectionLayer = document.getElementById('selection-layer');
      const selectionText = document.getElementById('selection-text');
      const selectionClose = document.getElementById('selection-close');
      const suppressDetachedInteraction = (event) => {
        if (selectionMode || remoteAtBottom) return false;

        try {
          if (terminal.textarea && document.activeElement === terminal.textarea) {
            terminal.textarea.blur();
          }
        } catch (_) {}

        if (event && event.cancelable) {
          event.preventDefault();
        }
        if (event && typeof event.stopImmediatePropagation === 'function') {
          event.stopImmediatePropagation();
        }
        if (event && typeof event.stopPropagation === 'function') {
          event.stopPropagation();
        }
        return true;
      };
      const dispatchTouchCursorMove = (clientX, clientY) => {
        if (!Number.isFinite(clientX) || !Number.isFinite(clientY)) return;

        try {
          terminal.clearSelection();
          terminal.focus();

          const terminalElement = terminal.element;
          const fallbackTarget =
            (terminalElement && terminalElement.querySelector('.xterm-screen')) ||
            terminalElement ||
            document.getElementById('terminal');
          const target = document.elementFromPoint(clientX, clientY) || fallbackTarget;
          if (!target) return;

          const common = {
            bubbles: true,
            cancelable: true,
            composed: true,
            view: window,
            clientX,
            clientY,
            button: 0,
            detail: 1,
            altKey: true,
          };

          target.dispatchEvent(new MouseEvent('mousedown', {
            ...common,
            buttons: 1,
          }));
          target.dispatchEvent(new MouseEvent('mouseup', {
            ...common,
            buttons: 0,
          }));
        } catch (_) {}
      };

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

      const snapshotVisibleText = () => {
        try {
          const buf = terminal.buffer.active;
          const lines = [];
          for (let i = 0; i < terminal.rows; i++) {
            const line = buf.getLine(buf.viewportY + i);
            lines.push(line ? line.translateToString(true) : '');
          }
          while (lines.length > 0 && lines[lines.length - 1].trim() === '') lines.pop();
          return lines.join('\n');
        } catch(_) {
          return '';
        }
      };

      const closeSelectionMode = () => {
        selectionMode = false;
        if (selectionLayer) selectionLayer.classList.remove('active');
        if (selectionText) selectionText.textContent = '';
        const selection = window.getSelection();
        if (selection) selection.removeAllRanges();
      };

      const openSelectionMode = () => {
        const text = snapshotVisibleText().trimEnd();
        if (!text || !selectionLayer || !selectionText) return;

        selectionMode = true;
        selectionText.textContent = text;
        selectionLayer.classList.add('active');

        requestAnimationFrame(() => {
          try {
            const selection = window.getSelection();
            if (!selection) return;
            const range = document.createRange();
            range.selectNodeContents(selectionText);
            selection.removeAllRanges();
            selection.addRange(range);
          } catch (_) {}
        });
      };

      if (selectionClose) {
        selectionClose.addEventListener('click', (e) => {
          e.preventDefault();
          e.stopPropagation();
          closeSelectionMode();
        });
      }

      if (selectionLayer) {
        selectionLayer.addEventListener('click', (e) => {
          if (e.target === selectionLayer) {
            closeSelectionMode();
          }
        });
      }

      for (const eventName of ['pointerdown', 'pointerup', 'mousedown', 'mouseup', 'click']) {
        document.addEventListener(eventName, (event) => {
          suppressDetachedInteraction(event);
        }, { capture: true, passive: false });
      }

      document.addEventListener('focusin', () => {
        if (selectionMode || remoteAtBottom) return;
        try {
          if (terminal.textarea && document.activeElement === terminal.textarea) {
            terminal.textarea.blur();
          }
        } catch (_) {}
      }, { capture: true });

      // ── Touch scroll engine ─────────────────────────────────
      //
      // Scrolling: delegated to daemon via tmux copy-mode.
      // Long-press (500ms): swaps in a selectable text layer so Android's
      // native text-selection toolbar can handle copy.
      //
      (function initTouchScroll() {
        let scrolling = false;
        let longPressTriggered = false;
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
        const THROTTLE_MS = 60;
        const LONG_PRESS_MS = 500;

        // ── Throttled scroll sender ──
        let pendingLines = 0;
        let throttleTimer = null;

        const flushScroll = () => {
          throttleTimer = null;
          if (pendingLines === 0) return;
          send({ type: 'scroll', lines: pendingLines });
          pendingLines = 0;
        };

        const doScroll = (lines) => {
          if (lines === 0) return;
          pendingLines += lines;
          if (!throttleTimer) {
            throttleTimer = setTimeout(flushScroll, THROTTLE_MS);
          }
        };

        const flushScrollNow = () => {
          if (throttleTimer) {
            clearTimeout(throttleTimer);
            throttleTimer = null;
          }
          if (pendingLines !== 0) {
            send({ type: 'scroll', lines: pendingLines });
            pendingLines = 0;
          }
        };

        const cancelMomentum = () => {
          if (momentumRAF) {
            cancelAnimationFrame(momentumRAF);
            momentumRAF = null;
          }
        };

        let longPressTimer = null;

        const cancelLongPress = () => {
          if (longPressTimer) {
            clearTimeout(longPressTimer);
            longPressTimer = null;
          }
        };

        document.addEventListener('touchstart', (e) => {
          if (selectionMode) return;
          cancelMomentum();
          scrolling = false;
          longPressTriggered = false;
          scrollAccum = 0;
          velocity = 0;
          startY = lastY = e.touches[0].clientY;
          lastTime = performance.now();

          cancelLongPress();
          longPressTimer = setTimeout(() => {
            longPressTimer = null;
            if (!scrolling) {
              longPressTriggered = true;
              openSelectionMode();
            }
          }, LONG_PRESS_MS);
        }, { capture: true, passive: true });

        document.addEventListener('touchmove', (e) => {
          if (selectionMode) return;
          const y = e.touches[0].clientY;
          if (!scrolling && Math.abs(startY - y) > THRESHOLD) {
            scrolling = true;
            scrollAccum = 0;
            cancelLongPress();
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

        document.addEventListener('touchend', (e) => {
          if (selectionMode) return;
          cancelLongPress();
          if (longPressTriggered) {
            scrolling = false;
            longPressTriggered = false;
            return;
          }

          // Short tap (no scroll, no long-press) should only reach React Native
          // when the terminal is already at the live bottom. Otherwise RN focuses
          // the hidden textarea, which exits copy-mode and pops the keyboard.
          if (!scrolling) {
            if (suppressDetachedInteraction(e)) {
              return;
            }
            if (remoteAtBottom) {
              const touch = e.changedTouches && e.changedTouches[0];
              if (touch) {
                dispatchTouchCursorMove(touch.clientX, touch.clientY);
              }
            }
            return;
          }
          scrolling = false;

          flushScrollNow();

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
              flushScrollNow();
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
        }, { capture: true, passive: false });

        document.addEventListener('touchcancel', () => {
          if (selectionMode) return;
          scrolling = false;
          longPressTriggered = false;
          cancelLongPress();
          cancelMomentum();
          flushScrollNow();
        }, { capture: true, passive: true });
      })();

      // ── Write queue (chunked output) ────────────────────────
      // Initial buffering: when tmux attach-session runs, it redraws
      // the entire screen via cursor positioning. Without buffering
      // the user sees a top-to-bottom "scroll" effect. We accumulate
      // all output for the first 300ms, then write it in one shot so
      // the terminal appears instantly with the final state.
      const INITIAL_BUFFER_MS = 300;
      let initialBuffer = '';
      let initialBuffering = true;
      let initialTimer = null;

      const flushInitialBuffer = () => {
        initialBuffering = false;
        initialTimer = null;
        if (initialBuffer) {
          terminal.write(initialBuffer);
          initialBuffer = '';
        }
      };

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

        // During initial buffering, accumulate everything
        if (initialBuffering) {
          initialBuffer += data;
          if (!initialTimer) {
            initialTimer = setTimeout(flushInitialBuffer, INITIAL_BUFFER_MS);
          }
          return;
        }

        const maxChunkSize = 4096;
        for (let i = 0; i < data.length; i += maxChunkSize) {
          writeQueue.push(data.slice(i, i + maxChunkSize));
        }
        if (!writeScheduled) requestAnimationFrame(flushWriteQueue);
      };

      // ── Bridge API ──────────────────────────────────────────
      window.__zenOutput = (data) => enqueueOutput(data);
      window.__zenFocus = () => {
        if (!remoteAtBottom) return;
        terminal.focus();
      };
      window.__zenScrollToBottom = () => {
        terminal.scrollToBottom();
      };
      window.__zenScrollState = (state) => {
        remoteAtBottom = !!(state && state.atBottom);
        terminal.options.disableStdin = !remoteAtBottom;
        if (terminal.textarea) {
          terminal.textarea.readOnly = !remoteAtBottom;
          if (!remoteAtBottom && document.activeElement === terminal.textarea) {
            terminal.textarea.blur();
          }
        }
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
