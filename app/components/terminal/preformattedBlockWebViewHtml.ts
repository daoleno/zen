export interface PreformattedBlockHtmlOptions {
  text: string;
  color: string;
  fontSize: number;
  lineHeight: number;
  paddingHorizontal: number;
  paddingVertical: number;
}

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

export function buildPreformattedBlockHtml(options: PreformattedBlockHtmlOptions): string {
  const {
    text,
    color,
    fontSize,
    lineHeight,
    paddingHorizontal,
    paddingVertical,
  } = options;
  const body = escapeHtml(text.replace(/\r\n/g, "\n"));

  return String.raw`<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta
      id="viewport"
      name="viewport"
      content="width=2048, initial-scale=1, maximum-scale=1, user-scalable=no"
    />
    <style>
      html, body {
        margin: 0;
        padding: 0;
        background: transparent;
        overflow: hidden;
        -webkit-text-size-adjust: none;
        text-size-adjust: none;
      }
      #wrap {
        display: inline-block;
        box-sizing: border-box;
        padding: ${paddingVertical}px ${paddingHorizontal}px;
      }
      pre {
        margin: 0;
        color: ${color};
        font-family: Menlo, Monaco, "Courier New", monospace;
        font-size: ${fontSize}px;
        line-height: ${lineHeight}px;
        white-space: pre;
        tab-size: 4;
        font-variant-ligatures: none;
        letter-spacing: 0;
        user-select: text;
        -webkit-user-select: text;
        -webkit-touch-callout: default;
      }
    </style>
  </head>
  <body>
    <div id="wrap">
      <pre id="content">${body}</pre>
    </div>
    <script>
      const PADDING_X = ${paddingHorizontal};
      const sendSize = () => {
        const wrap = document.getElementById("wrap");
        const pre = document.getElementById("content");
        if (!wrap || !pre) {
          return;
        }
        const width = Math.ceil(pre.scrollWidth + PADDING_X * 2);
        const height = Math.ceil(wrap.getBoundingClientRect().height);
        const viewport = document.getElementById("viewport");
        if (viewport) {
          viewport.setAttribute(
            "content",
            "width=" + width + ", initial-scale=1, maximum-scale=1, user-scalable=no",
          );
        }
        document.documentElement.style.width = width + "px";
        document.body.style.width = width + "px";
        try {
          window.ReactNativeWebView.postMessage(JSON.stringify({
            type: "size",
            width,
            height,
          }));
        } catch (_) {}
      };

      sendSize();
      window.addEventListener("load", sendSize);
      if (typeof ResizeObserver !== "undefined") {
        const pre = document.getElementById("content");
        if (pre) {
          new ResizeObserver(sendSize).observe(pre);
        }
      }
    </script>
  </body>
</html>`;
}