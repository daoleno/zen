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
    </style>
  </head>
  <body>
    <div id="root">
      <div id="terminal-html"></div>
      <div id="terminal-cursor"></div>
      <span id="cell-measure">M</span>
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

        if (!root || !terminalHtml || !cursor || !measure) {
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
        let nativeSelectionActive = false;
        let cursorBlinkVisible = true;
        let drawRAF = null;

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
          if (nativeSelectionActive) {
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
        };

        const updateCursor = () => {
          if (
            viewportMode !== 'live' ||
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

        const clearSelection = () => {
          const selection = window.getSelection();
          if (selection) {
            selection.removeAllRanges();
          }
          syncNativeSelectionState();
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

          return (
            selectionContainsNode(selection.anchorNode) ||
            selectionContainsNode(selection.focusNode)
          );
        };

        const syncNativeSelectionState = () => {
          const nextActive = hasTerminalSelection();
          if (nativeSelectionActive === nextActive) {
            return nextActive;
          }

          nativeSelectionActive = nextActive;
          send({ type: 'selectionActive', active: nextActive });
          scheduleDraw();
          return nextActive;
        };

        const VERTICAL_START_THRESHOLD = 4;
        const VERTICAL_LOCK_RATIO = 0.9;

        let scrolling = false;
        let startX = 0;
        let startY = 0;
        let lastY = 0;
        let scrollAccum = 0;
        let pendingLines = 0;
        let scrollFlushRAF = null;
        let scrollGestureActive = false;

        const flushScroll = () => {
          scrollFlushRAF = null;
          if (pendingLines === 0) {
            return;
          }
          // Match native touch scrolling expectations: dragging downward should
          // pull older history into view.
          send({ type: 'scroll', lines: pendingLines });
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

        const beginScrollGesture = () => {
          if (scrollGestureActive) {
            return;
          }
          scrollGestureActive = true;
          send({ type: 'scrollStart' });
        };

        const endScrollGesture = () => {
          if (!scrollGestureActive) {
            return;
          }
          flushScrollNow();
          scrollGestureActive = false;
          send({ type: 'scrollEnd' });
        };

        document.addEventListener('touchstart', (event) => {
          if (nativeSelectionActive) {
            return;
          }
          scrolling = false;
          scrollAccum = 0;
          startX = event.touches[0].clientX;
          startY = lastY = event.touches[0].clientY;
          scheduleDraw();
        }, { capture: true, passive: true });

        document.addEventListener('touchmove', (event) => {
          if (nativeSelectionActive) {
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
            beginScrollGesture();
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
        }, { capture: true, passive: false });

        document.addEventListener('touchend', (event) => {
          if (nativeSelectionActive) {
            return;
          }

          const touch = event.changedTouches && event.changedTouches[0];
          const endX = touch ? touch.clientX : startX;
          const endY = touch ? touch.clientY : startY;

          if (!scrolling) {
            if (syncNativeSelectionState()) {
              return;
            }
            emitTap(endX, endY);
            return;
          }

          scrolling = false;
          endScrollGesture();
          scrollAccum = 0;
        }, { capture: true, passive: false });

        document.addEventListener('touchcancel', () => {
          if (nativeSelectionActive) {
            return;
          }
          scrolling = false;
          endScrollGesture();
          scrollAccum = 0;
        }, { capture: true, passive: true });

        document.addEventListener('selectionchange', () => {
          syncNativeSelectionState();
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
          }
          scheduleDraw();
        };

        window.__zenBlur = () => {
          clearSelection();
        };

        window.__zenResumeInput = () => {
          clearSelection();
          viewportMode = 'live';
          scrollAccum = 0;
          scheduleDraw();
        };

        window.__zenScrollToBottom = () => {
          clearSelection();
          viewportMode = 'live';
          scrollAccum = 0;
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
