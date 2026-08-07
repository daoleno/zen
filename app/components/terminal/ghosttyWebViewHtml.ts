import type { TerminalThemePalette } from '../../constants/terminalThemes';
import {
  TERMINAL_GRID_CELL_WIDTH_FALLBACK_EM,
  TERMINAL_GRID_FONT_SIZE_CSS_PX,
  TERMINAL_GRID_LINE_HEIGHT_RATIO,
  TERMINAL_GRID_SIZE_SOURCE,
  TERMINAL_TEXT_SIZE_ADJUST_CSS,
  terminalGridLineHeightCssPx,
} from './terminalFontDensity';
import { TERMINAL_SCROLL_GESTURE_CONTROLLER_SOURCE } from './terminalScrollGesture';

export function buildGhosttyTerminalHtml(
  theme: TerminalThemePalette,
  fontUri: string | null,
  fontSize: number,
  rendererGeneration: number,
) {
  const terminalFontSize = Number.isFinite(fontSize) && fontSize > 0
    ? fontSize
    : TERMINAL_GRID_FONT_SIZE_CSS_PX;
  const lineHeight = terminalGridLineHeightCssPx(terminalFontSize);
  const safeRendererGeneration = Number.isSafeInteger(rendererGeneration) &&
    rendererGeneration >= 0
    ? rendererGeneration
    : 0;
  const escapedFontUri = fontUri
    ?.replace(/\\/g, '\\\\')
    .replace(/'/g, "\\'") ?? null;
  const fontFace = escapedFontUri ? `
    @font-face {
      font-family: 'ZenTerm';
      src: url('${escapedFontUri}') format('truetype');
      font-display: swap;
    }
  ` : '';
  const terminalFontFamily = escapedFontUri ? "'ZenTerm', monospace" : 'monospace';

  return String.raw`<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta
      name="viewport"
      content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no"
    />
    <style>
      ${fontFace}
      html, body {
        margin: 0;
        padding: 0;
        width: 100%;
        height: 100%;
        overflow: hidden;
        background: ${theme.background};
        overscroll-behavior: none;
        ${TERMINAL_TEXT_SIZE_ADJUST_CSS}
      }
      body {
        user-select: none;
        -webkit-user-select: none;
      }
      #root {
        position: relative;
        width: 100%;
        height: 100%;
        overflow: hidden;
        background: ${theme.background};
      }
      #terminal-html {
        position: absolute;
        inset: 0;
        overflow: hidden;
        display: block;
        box-sizing: border-box;
        background: ${theme.background};
        color: ${theme.foreground};
        font-family: ${terminalFontFamily};
        font-size: ${terminalFontSize}px;
        line-height: ${lineHeight}px;
        white-space: normal;
        tab-size: 8;
        pointer-events: auto;
        user-select: text;
        -webkit-user-select: text;
        -webkit-touch-callout: default;
        touch-action: none;
        -webkit-tap-highlight-color: transparent;
        transform: translate3d(0, 0, 0);
      }
      #terminal-html * {
        font-family: inherit;
        user-select: text;
        -webkit-user-select: text;
      }
      #terminal-html .terminal-row {
        display: block;
        height: ${lineHeight}px;
        line-height: ${lineHeight}px;
        white-space: pre;
      }
      #terminal-html .terminal-row * {
        white-space: pre;
      }
      #terminal-html pre {
        margin: 0;
        white-space: pre;
      }
      #terminal-html::selection,
      #terminal-html *::selection {
        background: ${theme.selectionBackground};
      }
      #terminal-cursor {
        position: absolute;
        top: 0;
        left: 0;
        z-index: 10;
        display: none;
        width: 2px;
        height: ${lineHeight}px;
        background: ${theme.cursor};
        pointer-events: none;
      }
      #cell-measure {
        position: absolute;
        visibility: hidden;
        white-space: pre;
        font-family: ${terminalFontFamily};
        font-size: ${terminalFontSize}px;
        line-height: ${lineHeight}px;
      }
    </style>
  </head>
  <body>
    <div id="root">
      <div id="terminal-html"></div>
      <div id="terminal-cursor"></div>
      <span id="cell-measure">M</span>
    </div>
    <script>
      const FONT_SIZE = ${terminalFontSize};
      const LINE_HEIGHT_RATIO = ${TERMINAL_GRID_LINE_HEIGHT_RATIO};
      const CELL_WIDTH_FALLBACK = ${TERMINAL_GRID_CELL_WIDTH_FALLBACK_EM};
      const LINE_HEIGHT_PX = Math.ceil(FONT_SIZE * LINE_HEIGHT_RATIO);
      const HAS_BUNDLED_FONT = ${escapedFontUri !== null};
      const FONT_READY_TIMEOUT_MS = 1200;
      const RENDERER_GENERATION = ${safeRendererGeneration};

      ${TERMINAL_GRID_SIZE_SOURCE}

      let rendererReady = false;
      const send = (payload) => {
        try {
          payload.rendererGeneration = RENDERER_GENERATION;
          window.ReactNativeWebView.postMessage(JSON.stringify(payload));
        } catch (_) {}
      };

      const errorMessage = (value) => {
        if (value && typeof value.message === 'string' && value.message) {
          return value.message;
        }
        if (typeof value === 'string' && value) {
          return value;
        }
        try {
          return String(value || 'Unknown WebView script failure');
        } catch (_) {
          return 'Unknown WebView script failure';
        }
      };

      const reportScriptIssue = (type, value) => {
        send({ type, message: errorMessage(value) });
      };

      window.addEventListener('error', (event) => {
        reportScriptIssue(
          rendererReady ? 'runtimeError' : 'bootstrapError',
          event.error || event.message,
        );
      });
      window.addEventListener('unhandledrejection', (event) => {
        reportScriptIssue(
          rendererReady ? 'runtimeError' : 'bootstrapError',
          event.reason,
        );
      });

      const waitForBundledFont = async () => {
        if (!HAS_BUNDLED_FONT) {
          return;
        }
        if (!document.fonts || typeof document.fonts.load !== 'function') {
          send({
            type: 'bootstrapWarning',
            message: 'WebView FontFaceSet is unavailable; using monospace fallback.',
          });
          return;
        }

        let timeoutId = null;
        const fontAttempt = Promise.resolve()
          .then(() => document.fonts.load(FONT_SIZE + 'px "ZenTerm"'))
          .then(() => document.fonts.ready)
          .then(
            () => ({ status: 'ready' }),
            (error) => ({ status: 'failed', error }),
          );
        const timeout = new Promise((resolve) => {
          timeoutId = setTimeout(
            () => resolve({ status: 'timeout' }),
            FONT_READY_TIMEOUT_MS,
          );
        });
        const result = await Promise.race([fontAttempt, timeout]);
        if (timeoutId != null) {
          clearTimeout(timeoutId);
        }
        if (result.status === 'failed') {
          send({
            type: 'bootstrapWarning',
            message: 'Bundled terminal font failed inside WebView; using monospace fallback: ' +
              errorMessage(result.error),
          });
        } else if (result.status === 'timeout') {
          send({
            type: 'bootstrapWarning',
            message: 'Bundled terminal font timed out; continuing with monospace fallback.',
          });
        }
      };

      (async () => {
        await waitForBundledFont();

        const root = document.getElementById('root');
        const terminalHtml = document.getElementById('terminal-html');
        const cursor = document.getElementById('terminal-cursor');
        const measure = document.getElementById('cell-measure');
        if (!root || !terminalHtml || !cursor || !measure) {
          throw new Error('Terminal WebView bootstrap elements are missing.');
        }

        let activeTheme = ${JSON.stringify(theme)};
        let renderSnapshot = {
          rows: 0,
          cols: 0,
          html: '',
          cursorCol: 0,
          cursorRow: 0,
          cursorVisible: false,
        };
        let viewportWidth = 1;
        let viewportHeight = 1;
        let cellWidth = Math.max(1, FONT_SIZE * CELL_WIDTH_FALLBACK);
        let cellHeight = LINE_HEIGHT_PX;
        let lastRenderedHtml = '';
        let lastReportedCols = 0;
        let lastReportedRows = 0;
        let lastReportedCellWidth = 0;
        let lastReportedCellHeight = 0;
        let nativeSelectionActive = false;
        let pendingViewportSyncAfterSelection = false;
        let cursorBlinkVisible = true;
        let drawRAF = null;
        let scrollRAF = null;
        let scrollSessionId = null;
        let scrollToken = null;

        const scheduleDraw = () => {
          if (drawRAF == null) {
            drawRAF = requestAnimationFrame(draw);
          }
        };

        const focusInput = () => {
          if (!nativeSelectionActive) {
            send({ type: 'focusInput', sessionId: scrollSessionId });
          }
        };

        const sendMouse = (action, button, x, y, anyButtonPressed) => {
          send({
            type: 'mouse',
            sessionId: scrollSessionId,
            action,
            button,
            x,
            y,
            anyButtonPressed,
          });
        };

        const emitTap = (x, y) => {
          if (nativeSelectionActive || hasTerminalSelection()) {
            return;
          }
          sendMouse('press', 'left', x, y, true);
          sendMouse('release', 'left', x, y, false);
          focusInput();
        };

        const measureCellWidth = () => {
          const width = measure.getBoundingClientRect().width;
          return Math.max(1, width || FONT_SIZE * CELL_WIDTH_FALLBACK);
        };

        const getViewportSize = () => {
          const rect = root.getBoundingClientRect();
          const width = rect.width || root.clientWidth ||
            document.documentElement.clientWidth || window.innerWidth;
          const height = rect.height || root.clientHeight ||
            document.documentElement.clientHeight || window.innerHeight;
          return {
            width: Math.max(1, Math.floor(width || 1)),
            height: Math.max(1, Math.floor(height || 1)),
          };
        };

        const applyTheme = () => {
          document.body.style.background = activeTheme.background;
          document.documentElement.style.background = activeTheme.background;
          root.style.background = activeTheme.background;
          terminalHtml.style.background = activeTheme.background;
          terminalHtml.style.color = activeTheme.foreground;
          cursor.style.background = activeTheme.cursor;
        };

        const updateCursor = () => {
          if (
            nativeSelectionActive ||
            !cursorBlinkVisible ||
            !renderSnapshot.cursorVisible
          ) {
            cursor.style.display = 'none';
            return;
          }

          const x = renderSnapshot.cursorCol * cellWidth;
          const y = renderSnapshot.cursorRow * cellHeight;
          if (x >= viewportWidth || y >= viewportHeight || x < 0 || y < 0) {
            cursor.style.display = 'none';
            return;
          }
          cursor.style.display = 'block';
          cursor.style.width = Math.max(2, Math.round(cellWidth * 0.14)) + 'px';
          cursor.style.height = cellHeight + 'px';
          cursor.style.transform = 'translate(' + x + 'px,' + y + 'px)';
        };

        const draw = () => {
          drawRAF = null;
          const nextHtml = renderSnapshot.html || '';
          if (!nativeSelectionActive && nextHtml !== lastRenderedHtml) {
            terminalHtml.innerHTML = nextHtml;
            lastRenderedHtml = nextHtml;
          }
          updateCursor();
        };

        const syncViewport = (force) => {
          if (nativeSelectionActive && !force) {
            pendingViewportSyncAfterSelection = true;
            return;
          }

          const viewport = getViewportSize();
          const nextCellWidth = measureCellWidth();
          const nextCellHeight = LINE_HEIGHT_PX;
          const nextGrid = gridSize(
            viewport.width,
            viewport.height,
            nextCellWidth,
            nextCellHeight,
          );
          const nextCols = nextGrid.cols;
          const nextRows = nextGrid.rows;
          viewportWidth = viewport.width;
          viewportHeight = viewport.height;
          cellWidth = nextCellWidth;
          cellHeight = nextCellHeight;

          const shouldReport = force ||
            nextCols !== lastReportedCols ||
            nextRows !== lastReportedRows ||
            Math.abs(nextCellWidth - lastReportedCellWidth) > 0.25 ||
            nextCellHeight !== lastReportedCellHeight;
          if (shouldReport) {
            lastReportedCols = nextCols;
            lastReportedRows = nextRows;
            lastReportedCellWidth = nextCellWidth;
            lastReportedCellHeight = nextCellHeight;
            send({
              type: 'resize',
              cols: nextCols,
              rows: nextRows,
              cellWidth: nextCellWidth,
              cellHeight: nextCellHeight,
            });
          }
          scheduleDraw();
        };

        const selectionContainsNode = (node) => {
          if (!node) {
            return false;
          }
          const element = node.nodeType === Node.TEXT_NODE ? node.parentNode : node;
          return element === terminalHtml || terminalHtml.contains(element);
        };

        const hasTerminalSelection = () => {
          const selection = window.getSelection();
          if (!selection || selection.isCollapsed || selection.rangeCount === 0) {
            return false;
          }
          return selectionContainsNode(selection.anchorNode) ||
            selectionContainsNode(selection.focusNode);
        };

        const clippedTextForRow = (range, row) => {
          const rowRange = document.createRange();
          rowRange.selectNodeContents(row);
          const clipped = range.cloneRange();
          if (clipped.compareBoundaryPoints(Range.START_TO_START, rowRange) < 0) {
            clipped.setStart(rowRange.startContainer, rowRange.startOffset);
          }
          if (clipped.compareBoundaryPoints(Range.END_TO_END, rowRange) > 0) {
            clipped.setEnd(rowRange.endContainer, rowRange.endOffset);
          }
          return clipped.toString();
        };

        const normalizedTerminalSelectionText = () => {
          const selection = window.getSelection();
          if (!selection || selection.isCollapsed || selection.rangeCount === 0) {
            return null;
          }
          if (
            !selectionContainsNode(selection.anchorNode) &&
            !selectionContainsNode(selection.focusNode)
          ) {
            return null;
          }

          const range = selection.getRangeAt(0);
          const rows = Array.from(terminalHtml.querySelectorAll('.terminal-row'));
          if (rows.length === 0) {
            return selection.toString();
          }
          const selected = [];
          for (const row of rows) {
            if (range.intersectsNode(row)) {
              selected.push({ row, text: clippedTextForRow(range, row) });
            }
          }
          if (selected.length === 0) {
            return selection.toString();
          }

          let text = '';
          for (let index = 0; index < selected.length; index += 1) {
            const current = selected[index];
            if (index > 0) {
              const previous = selected[index - 1].row;
              const previousSoftWraps = previous.dataset.wrap === '1';
              const currentContinuesWrap = current.row.dataset.wrapContinuation === '1';
              if (!previousSoftWraps && !currentContinuesWrap) {
                text += '\n';
              }
            }
            text += current.text;
          }
          return text;
        };

        const clearSelection = () => {
          const selection = window.getSelection();
          if (selection) {
            selection.removeAllRanges();
          }
          syncNativeSelectionState();
        };

        const scrollGesture = ${TERMINAL_SCROLL_GESTURE_CONTROLLER_SOURCE};

        const stopScrollFrame = () => {
          if (scrollRAF != null) {
            cancelAnimationFrame(scrollRAF);
            scrollRAF = null;
          }
        };

        const cancelScrollAnimation = (reason) => {
          scrollGesture.cancel(reason);
          stopScrollFrame();
        };

        const scheduleScrollFrame = () => {
          if (scrollRAF == null) {
            scrollRAF = requestAnimationFrame(flushScrollFrame);
          }
        };

        const flushScrollFrame = (timestamp) => {
          scrollRAF = null;
          const frame = scrollGesture.frame(timestamp);
          if (frame.lines !== 0) {
            send({
              type: 'scroll',
              sessionId: scrollSessionId,
              scrollToken,
              lines: frame.lines,
            });
          }
          if (frame.keepAnimating) {
            scrollRAF = requestAnimationFrame(flushScrollFrame);
          }
        };

        const syncNativeSelectionState = () => {
          const nextActive = hasTerminalSelection();
          if (nativeSelectionActive === nextActive) {
            return nextActive;
          }
          nativeSelectionActive = nextActive;
          if (nextActive) {
            scrollGesture.cancel('selection');
            stopScrollFrame();
          }
          send({ type: 'selectionActive', active: nextActive });
          if (!nextActive && pendingViewportSyncAfterSelection) {
            pendingViewportSyncAfterSelection = false;
            syncViewport(true);
          }
          scheduleDraw();
          return nextActive;
        };

        document.addEventListener('touchstart', (event) => {
          const touch = event.touches[0];
          cancelScrollAnimation('new-touch');
          scrollGesture.start(
            touch.clientX,
            touch.clientY,
            event.timeStamp,
            nativeSelectionActive,
          );
          scheduleDraw();
        }, { capture: true, passive: true });

        document.addEventListener('touchmove', (event) => {
          if (nativeSelectionActive) {
            return;
          }
          const touch = event.touches[0];
          if (!scrollGesture.move(
            touch.clientX,
            touch.clientY,
            event.timeStamp,
            cellHeight,
          )) {
            return;
          }
          if (event.cancelable) {
            event.preventDefault();
          }
          if (typeof event.stopPropagation === 'function') {
            event.stopPropagation();
          }
          scheduleScrollFrame();
        }, { capture: true, passive: false });

        document.addEventListener('touchend', (event) => {
          if (nativeSelectionActive) {
            cancelScrollAnimation('selection');
            return;
          }
          const touch = event.changedTouches && event.changedTouches[0];
          const endX = touch ? touch.clientX : 0;
          const endY = touch ? touch.clientY : 0;
          const claimed = scrollGesture.end(event.timeStamp);
          if (!claimed) {
            stopScrollFrame();
            if (!syncNativeSelectionState()) {
              emitTap(endX, endY);
            }
            return;
          }
          scheduleScrollFrame();
        }, { capture: true, passive: false });

        document.addEventListener('touchcancel', () => {
          cancelScrollAnimation('touch-cancel');
        }, { capture: true, passive: true });

        document.addEventListener('selectionchange', syncNativeSelectionState);

        document.addEventListener('copy', (event) => {
          const text = normalizedTerminalSelectionText();
          if (text == null) {
            return;
          }
          event.preventDefault();
          if (event.clipboardData) {
            event.clipboardData.setData('text/plain', text);
          }
          send({ type: 'copyText', text });
        });

        setInterval(() => {
          cursorBlinkVisible = !cursorBlinkVisible;
          scheduleDraw();
        }, 530);

        window.__zenRenderSnapshot = (nextSnapshot) => {
          renderSnapshot = nextSnapshot || renderSnapshot;
          scheduleDraw();
        };

        window.__zenTheme = (nextTheme) => {
          if (nextTheme) {
            activeTheme = nextTheme;
            applyTheme();
            scheduleDraw();
          }
        };

        window.__zenCancelScroll = (reason) => {
          scrollGesture.cancel(reason);
          stopScrollFrame();
        };

        window.__zenSetScrollContext = (sessionId, token, reason) => {
          cancelScrollAnimation(reason || 'session-change');
          scrollSessionId = typeof sessionId === 'string' && sessionId
            ? sessionId
            : null;
          scrollToken = typeof token === 'string' && token ? token : null;
        };

        window.__zenBlur = () => {
          cancelScrollAnimation('route-blur');
          clearSelection();
        };

        window.__zenWakeRenderer = () => {
          syncViewport(false);
          scheduleDraw();
          const rootEl = document.documentElement;
          const previousTransform = rootEl.style.transform;
          rootEl.style.transform = 'translateZ(0)';
          void rootEl.offsetHeight;
          rootEl.style.transform = previousTransform || '';
        };

        window.__zenResumeInput = () => {
          cancelScrollAnimation('jump-live');
          clearSelection();
          window.__zenWakeRenderer();
        };

        window.__zenScrollToBottom = () => {
          cancelScrollAnimation('jump-live');
          clearSelection();
          window.__zenWakeRenderer();
        };

        const handleViewportChange = () => syncViewport(false);
        window.addEventListener('resize', handleViewportChange);
        window.addEventListener('orientationchange', handleViewportChange);
        if (typeof ResizeObserver === 'function') {
          const observer = new ResizeObserver(handleViewportChange);
          observer.observe(root);
        }

        applyTheme();
        requestAnimationFrame(() => {
          try {
            syncViewport(true);
            rendererReady = true;
            send({ type: 'ready' });
          } catch (error) {
            reportScriptIssue('bootstrapError', error);
          }
        });
      })().catch((error) => {
        reportScriptIssue('bootstrapError', error);
      });
    </script>
  </body>
</html>`;
}
