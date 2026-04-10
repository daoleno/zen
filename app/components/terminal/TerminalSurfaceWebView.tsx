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
  | { type: 'scroll'; lines: number }
  | { type: 'ctrlConsumed' }
  | { type: 'tabSwipeProgress'; deltaX: number; active: boolean }
  | { type: 'tabSwipe'; direction: 'next' | 'prev' };

type NativeToTerminalMessage =
  | { type: 'output'; data: string }
  | { type: 'theme'; theme: TerminalThemePalette }
  | { type: 'session'; cols: number; rows: number }
  | { type: 'scrollState'; atBottom: boolean }
  | { type: 'ctrlState'; armed: boolean };

let cachedTerminalFontUri: string | null = null;

export interface TerminalSurfaceHandle {
  sendInput(data: string, options?: { focus?: boolean }): void;
  focus(): void;
  blur(): void;
  resumeInput(): void;
  scrollToBottom(): void;
}

interface TerminalSurfaceWebViewProps {
  serverId: string;
  targetId: string;
  backend?: string;
  themeName?: TerminalThemeName;
  themeOverrides?: Partial<TerminalThemePalette>;
  ctrlArmed?: boolean;
  onCtrlArmedChange?: (next: boolean) => void;
  onTabSwipeProgress?: (deltaX: number, active: boolean) => void;
  onTabSwipe?: (direction: 'next' | 'prev') => void;
}

export const TerminalSurfaceWebView = forwardRef<TerminalSurfaceHandle, TerminalSurfaceWebViewProps>(({
  serverId,
  targetId,
  backend = 'tmux',
  themeName = DefaultTerminalThemeName,
  themeOverrides,
  ctrlArmed = false,
  onCtrlArmedChange,
  onTabSwipeProgress,
  onTabSwipe,
}, ref) => {
  const webviewRef = useRef<WebView>(null);
  const pendingRef = useRef<NativeToTerminalMessage[]>([]);
  const initialSizeRef = useRef<{ cols: number; rows: number } | null>(null);
  const [fontUri, setFontUri] = useState<string | null>(cachedTerminalFontUri);
  const [ready, setReady] = useState(false);
  const [scrolledUp, setScrolledUp] = useState(false);
  const theme = useMemo(() => resolveTerminalTheme(themeName, themeOverrides), [themeName, themeOverrides]);

  const html = useMemo(() => (fontUri ? buildTerminalHtml(theme, fontUri) : ''), [theme, fontUri]);

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
    } else if (payload.type === 'ctrlState') {
      script = `window.__zenCtrlState && window.__zenCtrlState(${JSON.stringify({ armed: payload.armed })}); true;`;
    }
    if (script) {
      webviewRef.current?.injectJavaScript(script);
    }
  };

  const focusTerminal = () => {
    webviewRef.current?.injectJavaScript('window.__zenFocus && window.__zenFocus(); true;');
  };

  const blurTerminal = () => {
    webviewRef.current?.injectJavaScript('window.__zenBlur && window.__zenBlur(); true;');
  };

  const resumeTerminalInput = () => {
    webviewRef.current?.injectJavaScript('window.__zenResumeInput && window.__zenResumeInput(); true;');
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

  useEffect(() => {
    postToTerminal({ type: 'ctrlState', armed: ctrlArmed });
  }, [ctrlArmed]);

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
    sendInput(data: string, options?: { focus?: boolean }) {
      session.sendInput(data);
      if (options?.focus !== false) {
        focusTerminal();
      }
    },
    focus() {
      focusTerminal();
    },
    blur() {
      blurTerminal();
    },
    resumeInput() {
      session.cancelScroll();
      setScrolledUp(false);
      resumeTerminalInput();
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
      if (payload.type === 'ctrlConsumed') {
        onCtrlArmedChange?.(false);
        return;
      }
      if (payload.type === 'tabSwipeProgress') {
        onTabSwipeProgress?.(payload.deltaX, payload.active);
        return;
      }
      if (payload.type === 'tabSwipe') {
        onTabSwipe?.(payload.direction);
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
      {fontUri && (
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
      )}
      {(!fontUri || !ready) && (
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

function buildTerminalHtml(theme: TerminalThemePalette, fontUri: string) {
  const fontFace = `
      @font-face {
        font-family: 'ZenTerm';
        src: url('${fontUri}') format('truetype');
        font-display: swap;
      }
    `;
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
        top: 0;
        right: 0;
        bottom: auto;
        height: 100%;
        overflow: visible;
        pointer-events: none;
      }
      .xterm .xterm-helper-textarea {
        opacity: 0 !important;
        background: transparent !important;
        color: transparent !important;
        caret-color: transparent !important;
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
        font-family: ${JSON.stringify('ZenTerm, monospace')};
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

      (async () => {
        if (document.fonts && typeof document.fonts.load === 'function') {
          try {
            await document.fonts.load(FONT_SIZE + 'px "ZenTerm"');
            await document.fonts.ready;
          } catch (_) {}
        }

        const terminal = new Terminal({
          convertEol: false,
          cursorBlink: true,
          allowTransparency: false,
          fontFamily: ${JSON.stringify('ZenTerm, monospace')},
          fontSize: FONT_SIZE,
          lineHeight: LINE_HEIGHT_RATIO,
          letterSpacing: 0,
          theme: ${JSON.stringify(theme)},
          scrollback: 0
        });
        const fitAddon = new FitAddon.FitAddon();
        terminal.loadAddon(fitAddon);
        terminal.open(document.getElementById('terminal'));
        const core = terminal._core;
        const coreService = core && core.coreService;
        let ctrlArmed = false;
        const hideTextarea = () => {
          const textarea = terminal.textarea;
          if (!textarea) return;

          textarea.style.left = '-9999em';
          textarea.style.top = '0px';
          textarea.style.maxWidth = '0px';
          textarea.style.width = '0px';
          textarea.style.height = '0px';
          textarea.style.lineHeight = '';
          textarea.style.opacity = '0';
          textarea.style.pointerEvents = 'none';
          textarea.style.background = 'transparent';
          textarea.style.color = 'transparent';
          textarea.style.caretColor = 'transparent';
        };
        const clearCompositionArtifacts = () => {
          try {
            if (terminal.textarea) {
              terminal.textarea.value = '';
              hideTextarea();
            }
            if (core && core._compositionView) {
              core._compositionView.textContent = '';
              core._compositionView.classList.remove('active');
              core._compositionView.style.left = '0px';
              core._compositionView.style.top = '0px';
              core._compositionView.style.width = 'auto';
              core._compositionView.style.maxWidth = '';
              core._compositionView.style.height = 'auto';
              core._compositionView.style.minHeight = '';
              core._compositionView.style.lineHeight = '';
              core._compositionView.style.textIndent = '';
            }
          } catch (_) {}
        };
        hideTextarea();
        const patchCompositionLayout = () => {
          try {
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
                  textarea.style.opacity = '0';
                  textarea.style.pointerEvents = 'none';
                  textarea.style.background = 'transparent';
                  textarea.style.color = 'transparent';
                  textarea.style.caretColor = 'transparent';
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
        let swipeProgressDeltaX = 0;
        let swipeProgressActive = false;
        let swipeProgressRAF = null;
        let lastReportedCols = 0;
        let lastReportedRows = 0;
        let armRedrawBuffer = () => {};
        const flushSwipeProgress = () => {
          swipeProgressRAF = null;
          send({
            type: 'tabSwipeProgress',
            deltaX: swipeProgressDeltaX,
            active: swipeProgressActive,
          });
        };
        const reportSwipeProgress = (deltaX, active) => {
          swipeProgressDeltaX = deltaX;
          swipeProgressActive = active;
          if (swipeProgressRAF != null) return;
          swipeProgressRAF = requestAnimationFrame(flushSwipeProgress);
        };
        const reportSize = () => {
          try {
            fitAddon.fit();
            if (terminal.cols <= 0 || terminal.rows <= 0) {
              return;
            }
            if (terminal.cols === lastReportedCols && terminal.rows === lastReportedRows) {
              return;
            }
            if (initialized) {
              armRedrawBuffer();
            }
            lastReportedCols = terminal.cols;
            lastReportedRows = terminal.rows;
            send({ type: 'resize', cols: terminal.cols, rows: terminal.rows });
          } catch (_) {}
        };
        const clearCtrlArmed = () => {
          if (!ctrlArmed) return;
          ctrlArmed = false;
          send({ type: 'ctrlConsumed' });
        };
        const toCtrlSequence = (value) => {
          if (typeof value !== 'string' || value.length !== 1) return null;
          if (value === ' ') return '\x00';
          if (value === '?') return '\x7f';

          const code = value.charCodeAt(0);
          const upperCode = code >= 97 && code <= 122 ? code - 32 : code;
          if (upperCode >= 64 && upperCode <= 95) {
            return String.fromCharCode(upperCode - 64);
          }
          return null;
        };
        const flushCtrlCandidate = (value) => {
          if (!ctrlArmed || typeof value !== 'string' || value.length === 0) {
            return null;
          }
          const ctrlValue = toCtrlSequence(value);
          clearCtrlArmed();
          return ctrlValue ?? value;
        };
        if (coreService && typeof coreService.triggerDataEvent === 'function') {
          const originalTriggerDataEvent = coreService.triggerDataEvent.bind(coreService);
          coreService.triggerDataEvent = (data, wasUserInput) => {
            const nextData = flushCtrlCandidate(data);
            return originalTriggerDataEvent(nextData ?? data, wasUserInput);
          };
        }
        if (typeof terminal._inputEvent === 'function') {
          const originalInputEvent = terminal._inputEvent.bind(terminal);
          terminal._inputEvent = (ev) => {
            const nextData = flushCtrlCandidate(ev && typeof ev.data === 'string' ? ev.data : '');
            if (nextData != null) {
              clearCompositionArtifacts();
              send({ type: 'input', data: nextData });
              if (ev) {
                if (typeof terminal.cancel === 'function') {
                  terminal.cancel(ev);
                } else {
                  if (ev.preventDefault) ev.preventDefault();
                  if (ev.stopPropagation) ev.stopPropagation();
                }
              }
              return true;
            }
            return originalInputEvent(ev);
          };
        }
        if (core && core._compositionHelper && typeof core._compositionHelper.compositionend === 'function') {
          const helper = core._compositionHelper;
          const originalCompositionEnd = helper.compositionend.bind(helper);
          helper.compositionend = (...args) => {
            if (!ctrlArmed) {
              originalCompositionEnd(...args);
              setTimeout(clearCompositionArtifacts, 0);
              return;
            }

            const start = helper._compositionPosition && typeof helper._compositionPosition.start === 'number'
              ? helper._compositionPosition.start
              : 0;
            helper._compositionView.classList.remove('active');
            helper._isComposing = false;
            helper._isSendingComposition = false;

            setTimeout(() => {
              const textarea = terminal.textarea;
              const value = textarea ? textarea.value.substring(start) : '';
              const nextData = flushCtrlCandidate(value);
              if (textarea) {
                textarea.value = '';
              }
              clearCompositionArtifacts();
              if (nextData != null) {
                send({ type: 'input', data: nextData });
              }
            }, 0);
          };
        }

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
      const resumeLiveInput = () => {
        remoteAtBottom = true;
        terminal.options.disableStdin = false;
        clearCompositionArtifacts();
        terminal.scrollToBottom();
        if (terminal.textarea) {
          terminal.textarea.readOnly = false;
        }
        requestAnimationFrame(() => {
          try {
            terminal.focus();
          } catch (_) {}
        });
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
        let horizontalSwiping = false;
        let longPressTriggered = false;
        let startX = 0;
        let startY = 0;
        let lastY = 0;
        let lastTime = 0;
        let velocity = 0;
        let scrollAccum = 0;
        let momentumRAF = null;

        const VERTICAL_START_THRESHOLD = 12;
        const HORIZONTAL_START_THRESHOLD = 16;
        const HORIZONTAL_COMMIT_THRESHOLD = 64;
        const HORIZONTAL_LOCK_RATIO = 1.0;
        const VERTICAL_LOCK_RATIO = 1.1;
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
          horizontalSwiping = false;
          longPressTriggered = false;
          scrollAccum = 0;
          velocity = 0;
          startX = e.touches[0].clientX;
          startY = lastY = e.touches[0].clientY;
          lastTime = performance.now();

          cancelLongPress();
          longPressTimer = setTimeout(() => {
            longPressTimer = null;
            if (!scrolling && !horizontalSwiping) {
              longPressTriggered = true;
              openSelectionMode();
            }
          }, LONG_PRESS_MS);
        }, { capture: true, passive: true });

        document.addEventListener('touchmove', (e) => {
          if (selectionMode) return;
          const x = e.touches[0].clientX;
          const y = e.touches[0].clientY;
          const deltaXFromStart = x - startX;
          const deltaYFromStart = y - startY;
          const absDeltaX = Math.abs(deltaXFromStart);
          const absDeltaY = Math.abs(deltaYFromStart);

          if (!scrolling && !horizontalSwiping) {
            // Give diagonal gestures a fair chance before vertical scroll
            // claims the interaction.
            if (
              absDeltaX > HORIZONTAL_START_THRESHOLD &&
              absDeltaX >= absDeltaY * HORIZONTAL_LOCK_RATIO
            ) {
              horizontalSwiping = true;
              cancelLongPress();
            } else if (
              absDeltaY > VERTICAL_START_THRESHOLD &&
              absDeltaY > absDeltaX * VERTICAL_LOCK_RATIO
            ) {
              scrolling = true;
              scrollAccum = 0;
              cancelLongPress();
            }
          }

          if (horizontalSwiping) {
            reportSwipeProgress(deltaXFromStart, true);
            e.preventDefault();
            e.stopPropagation();
            return;
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
            horizontalSwiping = false;
            longPressTriggered = false;
            reportSwipeProgress(0, false);
            return;
          }

          if (horizontalSwiping) {
            const touch = e.changedTouches && e.changedTouches[0];
            const totalDeltaX = touch ? touch.clientX - startX : 0;
            horizontalSwiping = false;

            if (Math.abs(totalDeltaX) >= HORIZONTAL_COMMIT_THRESHOLD) {
              if (e.cancelable) e.preventDefault();
              if (typeof e.stopPropagation === 'function') e.stopPropagation();
              send({
                type: 'tabSwipe',
                direction: totalDeltaX < 0 ? 'next' : 'prev',
              });
            } else {
              reportSwipeProgress(0, false);
            }
            return;
          }

          // While reading scrollback, keep short taps inert so the view
          // doesn't unexpectedly snap back to live output.
          if (!scrolling) {
            if (suppressDetachedInteraction(e)) {
              return;
            }
            const touch = e.changedTouches && e.changedTouches[0];
            if (touch) {
              dispatchTouchCursorMove(touch.clientX, touch.clientY);
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
          horizontalSwiping = false;
          longPressTriggered = false;
          cancelLongPress();
          cancelMomentum();
          flushScrollNow();
          reportSwipeProgress(0, false);
        }, { capture: true, passive: true });
      })();

      // ── Write queue (chunked output) ────────────────────────
      // tmux redraws the whole visible screen on attach and on resize.
      // If we stream that redraw chunk-by-chunk, the user sees the screen
      // repaint from the top. Buffering the redraw briefly and flushing it
      // as a single write makes the terminal appear stable.
      const REDRAW_IDLE_MS = 72;
      const INITIAL_REDRAW_MAX_MS = 900;
      const RESIZE_REDRAW_MAX_MS = 320;
      let redrawBuffer = '';
      let redrawMode = 'initial';
      let redrawIdleTimer = null;
      let redrawDeadlineTimer = null;

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

      const clearRedrawTimers = () => {
        if (redrawIdleTimer) {
          clearTimeout(redrawIdleTimer);
          redrawIdleTimer = null;
        }
        if (redrawDeadlineTimer) {
          clearTimeout(redrawDeadlineTimer);
          redrawDeadlineTimer = null;
        }
      };

      const flushRedrawBuffer = () => {
        const buffered = redrawBuffer;
        redrawBuffer = '';
        redrawMode = null;
        clearRedrawTimers();
        if (!buffered) {
          return;
        }
        writeQueue.push(buffered);
        if (!writeScheduled) {
          requestAnimationFrame(flushWriteQueue);
        }
      };

      const scheduleRedrawFlush = () => {
        if (!redrawMode) {
          return;
        }
        if (redrawIdleTimer) {
          clearTimeout(redrawIdleTimer);
        }
        redrawIdleTimer = setTimeout(flushRedrawBuffer, REDRAW_IDLE_MS);
        if (!redrawDeadlineTimer) {
          const maxDelay = redrawMode === 'resize' ? RESIZE_REDRAW_MAX_MS : INITIAL_REDRAW_MAX_MS;
          redrawDeadlineTimer = setTimeout(flushRedrawBuffer, maxDelay);
        }
      };

      armRedrawBuffer = () => {
        redrawMode = initialized ? 'resize' : 'initial';
        clearRedrawTimers();
      };

      const enqueueChunkedWrite = (data) => {
        const maxChunkSize = 4096;
        for (let i = 0; i < data.length; i += maxChunkSize) {
          writeQueue.push(data.slice(i, i + maxChunkSize));
        }
        if (!writeScheduled) {
          requestAnimationFrame(flushWriteQueue);
        }
      };

      const enqueueOutput = (data) => {
        if (!data) return;

        if (redrawMode) {
          redrawBuffer += data;
          scheduleRedrawFlush();
          return;
        }

        enqueueChunkedWrite(data);
      };

      // ── Bridge API ──────────────────────────────────────────
      window.__zenOutput = (data) => enqueueOutput(data);
      window.__zenFocus = () => {
        if (!remoteAtBottom) return;
        terminal.focus();
      };
      window.__zenBlur = () => {
        try {
          if (terminal.textarea) {
            terminal.textarea.blur();
          }
        } catch (_) {}
        clearCompositionArtifacts();
      };
      window.__zenResumeInput = () => {
        resumeLiveInput();
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
        if (!remoteAtBottom) {
          clearCompositionArtifacts();
        }
      };
      window.__zenTheme = (t) => {
        terminal.options.theme = t;
        document.body.style.background = t.background;
        document.documentElement.style.background = t.background;
      };
      window.__zenCtrlState = (state) => {
        ctrlArmed = !!(state && state.armed);
        clearCompositionArtifacts();
      };
      window.__zenSession = () => {
        if (!initialized) {
          initialized = true;
          setTimeout(reportSize, 0);
        }
      };

      terminal.onData((data) => send({ type: 'input', data }));
      if (terminal.textarea && typeof terminal.textarea.addEventListener === 'function') {
        terminal.textarea.addEventListener('blur', clearCompositionArtifacts);
      }
      window.addEventListener('resize', reportSize);

      requestAnimationFrame(() => {
        reportSize();
        send({ type: 'ready' });
      });
      })();
    </script>
  </body>
</html>`;
}
