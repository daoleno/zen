import { Typography } from '../../constants/tokens';
import type { TerminalThemePalette } from '../../constants/terminalThemes';

export function buildGhosttyTerminalHtml(theme: TerminalThemePalette, fontUri: string) {
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
      ${fontFace}
      html, body {
        margin: 0;
        padding: 0;
        width: 100%;
        height: 100%;
        overflow: hidden;
        background: ${theme.background};
        overscroll-behavior: none;
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
        font-family: 'ZenTerm', monospace;
        font-size: ${Typography.terminalSize}px;
        line-height: ${Math.ceil(Typography.terminalSize * 1.28)}px;
        white-space: pre;
        tab-size: 8;
        pointer-events: none;
        transform: translate3d(0, 0, 0);
      }
      #terminal-html * {
        font-family: inherit;
      }
      #terminal-html pre {
        margin: 0;
        white-space: pre;
      }
      #terminal-cursor {
        position: absolute;
        top: 0;
        left: 0;
        z-index: 10;
        display: none;
        width: 2px;
        height: ${Math.ceil(Typography.terminalSize * 1.28)}px;
        background: ${theme.cursor};
        pointer-events: none;
      }
      #cell-measure {
        position: absolute;
        visibility: hidden;
        white-space: pre;
        font-family: 'ZenTerm', monospace;
        font-size: ${Typography.terminalSize}px;
        line-height: ${Math.ceil(Typography.terminalSize * 1.28)}px;
      }
      #selection-layer {
        display: none;
        position: fixed;
        inset: 0;
        z-index: 20;
        overflow: auto;
        box-sizing: border-box;
        padding: 14px 14px 22px;
        background: ${theme.background};
        color: ${theme.foreground};
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
        font-family: 'ZenTerm', monospace;
        font-size: ${Typography.terminalSize}px;
        line-height: ${Math.ceil(Typography.terminalSize * 1.28)}px;
      }
      #selection-text::selection {
        background: ${theme.selectionBackground};
      }
    </style>
  </head>
  <body>
    <div id="root">
      <div id="terminal-html"></div>
      <div id="terminal-cursor"></div>
      <span id="cell-measure">M</span>
      <div id="selection-layer">
        <button id="selection-close" type="button" aria-label="Close selection">×</button>
        <pre id="selection-text"></pre>
      </div>
    </div>
    <script>
      const FONT_SIZE = ${Typography.terminalSize};
      const LINE_HEIGHT_RATIO = 1.28;
      const CELL_WIDTH_FALLBACK = 0.62;
      const LINE_HEIGHT_PX = Math.ceil(FONT_SIZE * LINE_HEIGHT_RATIO);

      (async () => {
        if (document.fonts && typeof document.fonts.load === 'function') {
          try {
            await document.fonts.load(FONT_SIZE + 'px "ZenTerm"');
            await document.fonts.ready;
          } catch (_) {}
        }

        const root = document.getElementById('root');
        const terminalHtml = document.getElementById('terminal-html');
        const cursor = document.getElementById('terminal-cursor');
        const measure = document.getElementById('cell-measure');
        const selectionLayer = document.getElementById('selection-layer');
        const selectionClose = document.getElementById('selection-close');
        const selectionText = document.getElementById('selection-text');

        if (!root || !terminalHtml || !cursor || !measure || !selectionLayer || !selectionClose || !selectionText) {
          return;
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
        let viewportMode = 'live';
        let selectionMode = false;
        let cursorBlinkVisible = true;
        let drawRAF = null;
        let dragScrollOffsetPx = 0;

        const send = (payload) => {
          try {
            window.ReactNativeWebView.postMessage(JSON.stringify(payload));
          } catch (_) {}
        };

        const scheduleDraw = () => {
          if (drawRAF != null) {
            return;
          }
          drawRAF = requestAnimationFrame(draw);
        };

        const focusInput = () => {
          if (selectionMode) {
            return;
          }
          send({ type: 'focusInput' });
        };

        const sendMouse = (action, button, x, y, anyButtonPressed) => {
          send({
            type: 'mouse',
            action,
            button,
            x,
            y,
            anyButtonPressed,
          });
        };

        const emitTap = (x, y) => {
          if (selectionMode) {
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
          const width =
            rect.width ||
            root.clientWidth ||
            document.documentElement.clientWidth ||
            window.innerWidth;
          const height =
            rect.height ||
            root.clientHeight ||
            document.documentElement.clientHeight ||
            window.innerHeight;

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
          selectionLayer.style.background = activeTheme.background;
          selectionLayer.style.color = activeTheme.foreground;
          selectionClose.style.color = activeTheme.foreground;
        };

        const updateCursor = () => {
          if (
            viewportMode !== 'live' ||
            selectionMode ||
            Math.abs(dragScrollOffsetPx) >= 0.5 ||
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
          if (nextHtml !== lastRenderedHtml) {
            terminalHtml.innerHTML = nextHtml;
            lastRenderedHtml = nextHtml;
          }
          terminalHtml.style.transform = 'translate3d(0,' + dragScrollOffsetPx + 'px,0)';
          updateCursor();
        };

        const syncViewport = (force) => {
          const viewport = getViewportSize();
          const nextWidth = viewport.width;
          const nextHeight = viewport.height;
          const nextCellWidth = measureCellWidth();
          const nextCellHeight = LINE_HEIGHT_PX;
          const nextCols = Math.max(1, Math.floor(nextWidth / nextCellWidth));
          const nextRows = Math.max(1, Math.floor(nextHeight / nextCellHeight));

          viewportWidth = nextWidth;
          viewportHeight = nextHeight;
          cellWidth = nextCellWidth;
          cellHeight = nextCellHeight;

          const shouldReport =
            force ||
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

        const closeSelectionMode = () => {
          selectionMode = false;
          selectionLayer.classList.remove('active');
          selectionText.textContent = '';
          const selection = window.getSelection();
          if (selection) {
            selection.removeAllRanges();
          }
          scheduleDraw();
        };

        const openSelectionMode = (text) => {
          if (!text) {
            return;
          }

          selectionMode = true;
          selectionText.textContent = text;
          selectionLayer.classList.add('active');

          requestAnimationFrame(() => {
            try {
              const selection = window.getSelection();
              if (!selection) {
                return;
              }
              const range = document.createRange();
              range.selectNodeContents(selectionText);
              selection.removeAllRanges();
              selection.addRange(range);
            } catch (_) {}
          });

          scheduleDraw();
        };

        selectionClose.addEventListener('click', (event) => {
          event.preventDefault();
          event.stopPropagation();
          closeSelectionMode();
        });

        selectionLayer.addEventListener('click', (event) => {
          if (event.target === selectionLayer) {
            closeSelectionMode();
          }
        });

        const VERTICAL_START_THRESHOLD = 4;
        const VERTICAL_LOCK_RATIO = 0.9;
        const LONG_PRESS_MS = 500;

        let scrolling = false;
        let startX = 0;
        let startY = 0;
        let lastY = 0;
        let scrollAccum = 0;
        let pendingLines = 0;
        let scrollFlushRAF = null;
        let longPressTimer = null;
        let longPressTriggered = false;

        const flushScroll = () => {
          scrollFlushRAF = null;
          if (pendingLines === 0) {
            return;
          }
          // Match native touch scrolling expectations: dragging downward should
          // pull older history into view, so we invert the finger delta before
          // sending tmux's copy-mode scroll units.
          send({ type: 'scroll', lines: pendingLines });
          awaitingScrollPaint = true;
          pendingLines = 0;
        };

        const doScroll = (lines) => {
          if (lines === 0) {
            return;
          }
          pendingLines += lines;
          if (scrollFlushRAF == null) {
            scrollFlushRAF = requestAnimationFrame(flushScroll);
          }
        };

        const flushScrollNow = () => {
          if (scrollFlushRAF != null) {
            cancelAnimationFrame(scrollFlushRAF);
            scrollFlushRAF = null;
          }
          flushScroll();
        };

        const cancelLongPress = () => {
          if (longPressTimer) {
            clearTimeout(longPressTimer);
            longPressTimer = null;
          }
        };

        document.addEventListener('touchstart', (event) => {
          if (selectionMode) {
            return;
          }
          scrolling = false;
          longPressTriggered = false;
          scrollAccum = 0;
          dragScrollOffsetPx = 0;
          startX = event.touches[0].clientX;
          startY = lastY = event.touches[0].clientY;
          scheduleDraw();

          cancelLongPress();
          longPressTimer = setTimeout(() => {
            longPressTimer = null;
            if (!scrolling) {
              longPressTriggered = true;
              send({ type: 'requestSelection' });
            }
          }, LONG_PRESS_MS);
        }, { capture: true, passive: true });

        document.addEventListener('touchmove', (event) => {
          if (selectionMode) {
            return;
          }

          const x = event.touches[0].clientX;
          const y = event.touches[0].clientY;
          const deltaXFromStart = x - startX;
          const deltaYFromStart = y - startY;
          const absDeltaX = Math.abs(deltaXFromStart);
          const absDeltaY = Math.abs(deltaYFromStart);

          if (!scrolling) {
            if (absDeltaY <= VERTICAL_START_THRESHOLD || absDeltaY <= absDeltaX * VERTICAL_LOCK_RATIO) {
              return;
            }
            scrolling = true;
            scrollAccum = 0;
            cancelLongPress();
          }

          if (event.cancelable) {
            event.preventDefault();
          }
          if (typeof event.stopPropagation === 'function') {
            event.stopPropagation();
          }

          const delta = lastY - y;
          lastY = y;

          scrollAccum += delta;
          const lines = Math.trunc(scrollAccum / cellHeight);
          if (lines !== 0) {
            doScroll(lines);
            scrollAccum -= lines * cellHeight;
          }
          dragScrollOffsetPx = -scrollAccum;
          scheduleDraw();
        }, { capture: true, passive: false });

        document.addEventListener('touchend', (event) => {
          if (selectionMode) {
            return;
          }
          cancelLongPress();

          if (longPressTriggered) {
            scrolling = false;
            return;
          }

          const touch = event.changedTouches && event.changedTouches[0];
          const endX = touch ? touch.clientX : startX;
          const endY = touch ? touch.clientY : startY;

          if (!scrolling) {
            emitTap(endX, endY);
            return;
          }

          scrolling = false;
          flushScrollNow();
          scrollAccum = 0;
          dragScrollOffsetPx = 0;
          scheduleDraw();
        }, { capture: true, passive: false });

        document.addEventListener('touchcancel', () => {
          if (selectionMode) {
            return;
          }
          scrolling = false;
          longPressTriggered = false;
          cancelLongPress();
          flushScrollNow();
          scrollAccum = 0;
          dragScrollOffsetPx = 0;
          scheduleDraw();
        }, { capture: true, passive: true });

        setInterval(() => {
          cursorBlinkVisible = !cursorBlinkVisible;
          scheduleDraw();
        }, 530);

        window.__zenRenderSnapshot = (nextSnapshot) => {
          renderSnapshot = nextSnapshot || renderSnapshot;
          scheduleDraw();
        };

        window.__zenTheme = (nextTheme) => {
          if (!nextTheme) {
            return;
          }
          activeTheme = nextTheme;
          applyTheme();
          scheduleDraw();
        };

        window.__zenViewportMode = (state) => {
          viewportMode = state && state.mode === 'scrolled' ? 'scrolled' : 'live';
          if (viewportMode === 'live') {
            scrollAccum = 0;
            dragScrollOffsetPx = 0;
          }
          scheduleDraw();
        };

        window.__zenSelectionText = (payload) => {
          const text = payload && typeof payload.text === 'string'
            ? payload.text.trimEnd()
            : '';
          openSelectionMode(text);
        };

        window.__zenBlur = () => {
          closeSelectionMode();
        };

        window.__zenResumeInput = () => {
          closeSelectionMode();
          viewportMode = 'live';
          scrollAccum = 0;
          dragScrollOffsetPx = 0;
          scheduleDraw();
        };

        window.__zenScrollToBottom = () => {
          closeSelectionMode();
          viewportMode = 'live';
          scrollAccum = 0;
          dragScrollOffsetPx = 0;
          scheduleDraw();
        };

        const handleViewportChange = () => {
          syncViewport(false);
        };

        window.addEventListener('resize', handleViewportChange);
        window.addEventListener('orientationchange', handleViewportChange);

        if (typeof ResizeObserver === 'function') {
          const observer = new ResizeObserver(handleViewportChange);
          observer.observe(root);
        }

        applyTheme();

        requestAnimationFrame(() => {
          syncViewport(true);
          send({ type: 'ready' });
        });
      })();
    </script>
  </body>
</html>`;
}
