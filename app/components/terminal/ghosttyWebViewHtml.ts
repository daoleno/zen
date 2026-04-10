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
        background: ${theme.background};
      }
      #terminal-canvas {
        display: block;
        width: 100%;
        height: 100%;
        touch-action: none;
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
      <canvas id="terminal-canvas"></canvas>
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
      const FLAG_BOLD = 1 << 0;
      const FLAG_ITALIC = 1 << 1;
      const FLAG_UNDERLINE = 1 << 2;
      const FLAG_STRIKETHROUGH = 1 << 3;
      const FLAG_INVERSE = 1 << 4;

      (async () => {
        if (document.fonts && typeof document.fonts.load === 'function') {
          try {
            await document.fonts.load(FONT_SIZE + 'px "ZenTerm"');
            await document.fonts.ready;
          } catch (_) {}
        }

        const canvas = document.getElementById('terminal-canvas');
        const ctx = canvas.getContext('2d');
        const selectionLayer = document.getElementById('selection-layer');
        const selectionClose = document.getElementById('selection-close');
        const selectionText = document.getElementById('selection-text');

        if (!ctx || !selectionLayer || !selectionClose || !selectionText) {
          return;
        }

        let activeTheme = ${JSON.stringify(theme)};
        let renderState = {
          rows: 0,
          cols: 0,
          cells: [],
          cursorCol: 0,
          cursorRow: 0,
          cursorVisible: false,
        };
        let viewportWidth = 1;
        let viewportHeight = 1;
        let cellWidth = Math.max(1, FONT_SIZE * CELL_WIDTH_FALLBACK);
        let cellHeight = LINE_HEIGHT_PX;
        let lastReportedCols = 0;
        let lastReportedRows = 0;
        let lastReportedCellWidth = 0;
        let lastReportedCellHeight = 0;
        let remoteAtBottom = true;
        let selectionMode = false;
        let drawRAF = null;
        let swipeProgressRAF = null;
        let swipeProgressDeltaX = 0;
        let swipeProgressActive = false;
        let cursorBlinkVisible = true;
        let lastFont = '';

        const send = (payload) => {
          try {
            window.ReactNativeWebView.postMessage(JSON.stringify(payload));
          } catch (_) {}
        };

        const focusInput = () => {
          if (!remoteAtBottom || selectionMode) {
            return;
          }
          send({ type: 'focusInput' });
        };

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
          if (swipeProgressRAF != null) {
            return;
          }
          swipeProgressRAF = requestAnimationFrame(flushSwipeProgress);
        };

        const fontForFlags = (flags) => {
          const italic = (flags & FLAG_ITALIC) ? 'italic ' : '';
          const bold = (flags & FLAG_BOLD) ? 'bold ' : '';
          return italic + bold + FONT_SIZE + 'px ZenTerm, monospace';
        };

        const setFontForFlags = (flags) => {
          const nextFont = fontForFlags(flags);
          if (nextFont === lastFont) {
            return;
          }
          ctx.font = nextFont;
          lastFont = nextFont;
        };

        const argbToCss = (argb, fallback) => {
          if (!argb) {
            return fallback;
          }
          const value = argb >>> 0;
          const a = ((value >> 24) & 255) / 255;
          const r = (value >> 16) & 255;
          const g = (value >> 8) & 255;
          const b = value & 255;
          if (a >= 0.999) {
            return 'rgb(' + r + ',' + g + ',' + b + ')';
          }
          return 'rgba(' + r + ',' + g + ',' + b + ',' + a.toFixed(3) + ')';
        };

        const safeFromCodePoint = (value) => {
          if (!value) {
            return '';
          }
          try {
            return String.fromCodePoint(value);
          } catch (_) {
            return '';
          }
        };

        const scheduleDraw = () => {
          if (drawRAF != null) {
            return;
          }
          drawRAF = requestAnimationFrame(draw);
        };

        const drawCursor = () => {
          if (!remoteAtBottom || selectionMode || !cursorBlinkVisible || !renderState.cursorVisible) {
            return;
          }
          if (renderState.cursorCol < 0 || renderState.cursorRow < 0) {
            return;
          }
          const x = renderState.cursorCol * cellWidth;
          const y = renderState.cursorRow * cellHeight;
          if (x >= viewportWidth || y >= viewportHeight) {
            return;
          }
          ctx.fillStyle = activeTheme.cursor;
          ctx.fillRect(x, y, Math.max(2, Math.round(cellWidth * 0.14)), cellHeight);
        };

        const draw = () => {
          drawRAF = null;
          const rows = renderState.rows || 0;
          const cols = renderState.cols || 0;
          const cells = Array.isArray(renderState.cells) ? renderState.cells : [];
          const baselineOffset = Math.max(0, Math.floor((cellHeight - FONT_SIZE) / 2) - 1);

          ctx.clearRect(0, 0, viewportWidth, viewportHeight);
          ctx.fillStyle = activeTheme.background;
          ctx.fillRect(0, 0, viewportWidth, viewportHeight);
          ctx.textBaseline = 'top';
          ctx.direction = 'ltr';
          lastFont = '';

          for (let row = 0; row < rows; row += 1) {
            const y = row * cellHeight;
            for (let col = 0; col < cols; col += 1) {
              const offset = (row * cols + col) * 4;
              if (offset + 3 >= cells.length) {
                break;
              }

              const codepoint = cells[offset] | 0;
              const rawFg = cells[offset + 1] | 0;
              const rawBg = cells[offset + 2] | 0;
              const flags = cells[offset + 3] | 0;

              let fg = argbToCss(rawFg, activeTheme.foreground);
              let bg = rawBg ? argbToCss(rawBg, activeTheme.background) : activeTheme.background;
              if (flags & FLAG_INVERSE) {
                const swapped = fg;
                fg = bg;
                bg = swapped;
              }

              const x = col * cellWidth;
              if (bg !== activeTheme.background) {
                ctx.fillStyle = bg;
                ctx.fillRect(x, y, Math.ceil(cellWidth + 0.5), cellHeight);
              }

              if (codepoint > 0) {
                setFontForFlags(flags);
                ctx.fillStyle = fg;
                const glyph = safeFromCodePoint(codepoint);
                if (glyph) {
                  ctx.fillText(glyph, x, y + baselineOffset);
                }
                if (flags & FLAG_UNDERLINE) {
                  ctx.fillRect(x, y + cellHeight - 2, Math.max(1, cellWidth), 1);
                }
                if (flags & FLAG_STRIKETHROUGH) {
                  ctx.fillRect(x, y + Math.floor(cellHeight / 2), Math.max(1, cellWidth), 1);
                }
              }
            }
          }

          drawCursor();
        };

        const measureCellWidth = () => {
          ctx.save();
          ctx.font = '400 ' + FONT_SIZE + 'px ZenTerm, monospace';
          const width = ctx.measureText('M').width;
          ctx.restore();
          return Math.max(1, width || FONT_SIZE * CELL_WIDTH_FALLBACK);
        };

        const resizeCanvas = () => {
          const dpr = Math.max(1, window.devicePixelRatio || 1);
          canvas.width = Math.max(1, Math.floor(viewportWidth * dpr));
          canvas.height = Math.max(1, Math.floor(viewportHeight * dpr));
          canvas.style.width = viewportWidth + 'px';
          canvas.style.height = viewportHeight + 'px';
          ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
          ctx.textBaseline = 'top';
          ctx.direction = 'ltr';
          lastFont = '';
        };

        const syncViewport = (force) => {
          const rect = document.documentElement.getBoundingClientRect();
          const nextWidth = Math.max(1, Math.floor(rect.width));
          const nextHeight = Math.max(1, Math.floor(rect.height));
          const nextCellWidth = measureCellWidth();
          const nextCellHeight = LINE_HEIGHT_PX;
          const nextCols = Math.max(1, Math.floor(nextWidth / nextCellWidth));
          const nextRows = Math.max(1, Math.floor(nextHeight / nextCellHeight));
          const changed =
            force ||
            nextWidth !== viewportWidth ||
            nextHeight !== viewportHeight ||
            Math.abs(nextCellWidth - cellWidth) > 0.25 ||
            nextCellHeight !== cellHeight;

          viewportWidth = nextWidth;
          viewportHeight = nextHeight;
          cellWidth = nextCellWidth;
          cellHeight = nextCellHeight;

          if (changed) {
            resizeCanvas();
          }

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

        let scrolling = false;
        let horizontalSwiping = false;
        let startX = 0;
        let startY = 0;
        let lastY = 0;
        let lastTime = 0;
        let velocity = 0;
        let scrollAccum = 0;
        let pendingLines = 0;
        let throttleTimer = null;
        let momentumRAF = null;
        let longPressTimer = null;
        let longPressTriggered = false;

        const flushScroll = () => {
          throttleTimer = null;
          if (pendingLines === 0) {
            return;
          }
          send({ type: 'scroll', lines: pendingLines });
          pendingLines = 0;
        };

        const doScroll = (lines) => {
          if (lines === 0) {
            return;
          }
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
          cancelMomentum();
          scrolling = false;
          horizontalSwiping = false;
          longPressTriggered = false;
          scrollAccum = 0;
          velocity = 0;
          startX = event.touches[0].clientX;
          startY = lastY = event.touches[0].clientY;
          lastTime = performance.now();

          cancelLongPress();
          longPressTimer = setTimeout(() => {
            longPressTimer = null;
            if (!scrolling && !horizontalSwiping) {
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

          if (!scrolling && !horizontalSwiping) {
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
            if (event.cancelable) {
              event.preventDefault();
            }
            if (typeof event.stopPropagation === 'function') {
              event.stopPropagation();
            }
            return;
          }

          if (!scrolling) {
            return;
          }

          if (event.cancelable) {
            event.preventDefault();
          }
          if (typeof event.stopPropagation === 'function') {
            event.stopPropagation();
          }

          const delta = lastY - y;
          const now = performance.now();
          const dt = now - lastTime;

          if (dt > 0 && Number.isFinite(delta / dt)) {
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

        document.addEventListener('touchend', (event) => {
          if (selectionMode) {
            return;
          }
          cancelLongPress();

          if (longPressTriggered) {
            scrolling = false;
            horizontalSwiping = false;
            reportSwipeProgress(0, false);
            return;
          }

          if (horizontalSwiping) {
            const touch = event.changedTouches && event.changedTouches[0];
            const totalDeltaX = touch ? touch.clientX - startX : 0;
            horizontalSwiping = false;

            if (Math.abs(totalDeltaX) >= HORIZONTAL_COMMIT_THRESHOLD) {
              if (event.cancelable) {
                event.preventDefault();
              }
              if (typeof event.stopPropagation === 'function') {
                event.stopPropagation();
              }
              send({
                type: 'tabSwipe',
                direction: totalDeltaX < 0 ? 'next' : 'prev',
              });
            } else {
              reportSwipeProgress(0, false);
            }
            return;
          }

          if (!scrolling) {
            if (remoteAtBottom) {
              focusInput();
            }
            return;
          }

          scrolling = false;
          flushScrollNow();

          let frameVelocity = Math.max(-MAX_VELOCITY, Math.min(MAX_VELOCITY, velocity));
          if (Math.abs(frameVelocity) < MIN_MOMENTUM) {
            return;
          }

          let velocityPx = frameVelocity * 16;
          let accum = 0;
          let frames = 0;

          const tick = () => {
            frames += 1;
            if (Math.abs(velocityPx) < STOP_V || frames > MAX_FRAMES) {
              momentumRAF = null;
              flushScrollNow();
              return;
            }

            accum += velocityPx;
            const lines = Math.trunc(accum / LINE_HEIGHT_PX);
            if (lines !== 0) {
              doScroll(lines);
              accum -= lines * LINE_HEIGHT_PX;
            }
            velocityPx *= FRICTION;
            momentumRAF = requestAnimationFrame(tick);
          };

          momentumRAF = requestAnimationFrame(tick);
        }, { capture: true, passive: false });

        document.addEventListener('touchcancel', () => {
          if (selectionMode) {
            return;
          }
          scrolling = false;
          horizontalSwiping = false;
          longPressTriggered = false;
          cancelLongPress();
          cancelMomentum();
          flushScrollNow();
          reportSwipeProgress(0, false);
        }, { capture: true, passive: true });

        setInterval(() => {
          cursorBlinkVisible = !cursorBlinkVisible;
          scheduleDraw();
        }, 530);

        window.__zenRenderState = (nextState) => {
          renderState = nextState || renderState;
          scheduleDraw();
        };

        window.__zenTheme = (nextTheme) => {
          if (!nextTheme) {
            return;
          }
          activeTheme = nextTheme;
          document.body.style.background = activeTheme.background;
          document.documentElement.style.background = activeTheme.background;
          document.getElementById('root').style.background = activeTheme.background;
          selectionLayer.style.background = activeTheme.background;
          selectionLayer.style.color = activeTheme.foreground;
          selectionClose.style.color = activeTheme.foreground;
          scheduleDraw();
        };

        window.__zenScrollState = (state) => {
          remoteAtBottom = !!(state && state.atBottom);
          scheduleDraw();
        };

        window.__zenCtrlState = () => {};

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
          remoteAtBottom = true;
          send({ type: 'focusInput' });
          scheduleDraw();
        };

        window.__zenScrollToBottom = () => {
          closeSelectionMode();
          send({ type: 'focusInput' });
        };

        const handleViewportChange = () => {
          syncViewport(false);
        };

        window.addEventListener('resize', handleViewportChange);
        window.addEventListener('orientationchange', handleViewportChange);

        if (typeof ResizeObserver === 'function') {
          const observer = new ResizeObserver(handleViewportChange);
          observer.observe(document.documentElement);
        }

        requestAnimationFrame(() => {
          syncViewport(true);
          send({ type: 'ready' });
        });
      })();
    </script>
  </body>
</html>`;
}
