import { MERMAID_MAX_RENDER_MS, MERMAID_MESSAGE_VERSION } from "./mermaidLimits";
import { MERMAID_RUNTIME_SOURCE } from "./mermaidRuntimeSource";

export const MERMAID_ENGINE_BASE_URL = "https://zen.local/mermaid";

export const MERMAID_ENGINE_BOOTSTRAP = String.raw`
(function () {
  var VERSION = ${MERMAID_MESSAGE_VERSION};
  var MAX_RENDER_MS = ${MERMAID_MAX_RENDER_MS};
  var locked = false;
  function post(payload) {
    try {
      if (window.ReactNativeWebView && window.ReactNativeWebView.postMessage) {
        window.ReactNativeWebView.postMessage(JSON.stringify(payload));
      }
    } catch (_) {}
  }
  function fail(requestId, generation, error) {
    post({
      v: VERSION,
      type: "result",
      requestId: requestId,
      generation: generation,
      ok: false,
      error: String(error || "render failed").slice(0, 180)
    });
  }
  function svgSize(svg) {
    var match = /viewBox\s*=\s*["']\s*([\d.+-eE]+)\s+([\d.+-eE]+)\s+([\d.+-eE]+)\s+([\d.+-eE]+)\s*["']/i.exec(svg);
    if (match) {
      var width = Number(match[3]);
      var height = Number(match[4]);
      if (width > 0 && height > 0) {
        return { width: width, height: height };
      }
    }
    return { width: 320, height: 180 };
  }
  function lockMermaid() {
    if (!window.mermaid) {
      return false;
    }
    if (locked) {
      return true;
    }
    var originalInitialize = window.mermaid.initialize.bind(window.mermaid);
    window.mermaid.initialize = function (config) {
      var next = config && typeof config === "object" ? config : {};
      originalInitialize({
        startOnLoad: false,
        securityLevel: "strict",
        htmlLabels: false,
        maxTextSize: 32000,
        maxEdges: 120,
        fontFamily: "sans-serif",
        theme: next.theme === "dark" ? "dark" : "base",
        themeVariables: next.themeVariables || {},
        flowchart: {
          htmlLabels: false,
          useMaxWidth: true,
          curve: "basis",
          padding: 8
        }
      });
    };
    window.mermaid.initialize({});
    locked = true;
    return true;
  }
  window.__zenMermaidRender = function (payload) {
    if (!payload || payload.v !== VERSION || payload.type !== "render") {
      return;
    }
    if (typeof payload.requestId !== "string" || typeof payload.generation !== "number") {
      return;
    }
    if (typeof payload.source !== "string" || payload.source.length < 8) {
      fail(payload.requestId, payload.generation, "empty");
      return;
    }
    if (!lockMermaid()) {
      fail(payload.requestId, payload.generation, "engine");
      return;
    }
    var timeoutMs = typeof payload.timeoutMs === "number" && payload.timeoutMs > 0
      ? Math.min(payload.timeoutMs, MAX_RENDER_MS)
      : MAX_RENDER_MS;
    var theme = payload.theme && typeof payload.theme === "object" ? payload.theme : {};
    window.mermaid.initialize({
      theme: theme.mode === "dark" ? "dark" : "base",
      themeVariables: {
        background: "transparent",
        primaryColor: String(theme.primaryColor || "#eee"),
        primaryTextColor: String(theme.primaryTextColor || "#111"),
        primaryBorderColor: String(theme.primaryBorderColor || "#999"),
        lineColor: String(theme.lineColor || "#999"),
        secondaryColor: String(theme.secondaryColor || "#ddd"),
        tertiaryColor: String(theme.tertiaryColor || "#ccc"),
        clusterBkg: String(theme.clusterBkg || "#f4f4f4"),
        clusterBorder: String(theme.clusterBorder || "#999"),
        edgeLabelBackground: String(theme.edgeLabelBackground || "#fff"),
        fontFamily: "sans-serif",
        fontSize: String(theme.fontSize || "13px")
      }
    });
    try {
      var detected = window.mermaid.detectType(payload.source);
      if (detected !== "flowchart" && detected !== "flowchart-v2" && detected !== "graph") {
        fail(payload.requestId, payload.generation, "unsupported");
        return;
      }
    } catch (error) {
      fail(payload.requestId, payload.generation, error && error.message ? error.message : "parse");
      return;
    }
    var renderId = "zenm" + payload.generation.toString(10);
    var renderPromise = window.mermaid.render(renderId, payload.source);
    var timeout = new Promise(function (_, reject) {
      setTimeout(function () { reject(new Error("timeout")); }, timeoutMs);
    });
    Promise.race([renderPromise, timeout]).then(function (result) {
      var svg = result && result.svg ? String(result.svg) : "";
      if (!svg || svg.indexOf("<svg") === -1) {
        fail(payload.requestId, payload.generation, "blank");
        return;
      }
      if (/<script/i.test(svg)) {
        fail(payload.requestId, payload.generation, "unsafe");
        return;
      }
      var size = svgSize(svg);
      post({
        v: VERSION,
        type: "result",
        requestId: payload.requestId,
        generation: payload.generation,
        ok: true,
        svg: svg,
        width: size.width,
        height: size.height
      });
    }).catch(function (error) {
      fail(payload.requestId, payload.generation, error && error.message ? error.message : "render");
    });
  };
  if (!lockMermaid()) {
    post({ v: VERSION, type: "result", requestId: "boot", generation: 0, ok: false, error: "engine" });
    return;
  }
  post({ v: VERSION, type: "ready" });
})();
`;

export const MERMAID_ENGINE_CSP =
  "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'";

export function buildMermaidEngineHtml() {
  return `<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta http-equiv="Content-Security-Policy" content="${MERMAID_ENGINE_CSP}" />
    <meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no" />
    <style>
      html, body { margin: 0; padding: 0; background: transparent; overflow: hidden; }
    </style>
  </head>
  <body>
    <script>${MERMAID_RUNTIME_SOURCE}</script>
    <script>${MERMAID_ENGINE_BOOTSTRAP}</script>
  </body>
</html>`;
}

export function buildMermaidSvgPreviewHtml(svg: string, background: string) {
  const safeBackground = /^#?[0-9a-fA-F]{3,8}$/.test(background) || background === "transparent"
    ? background
    : "transparent";
  return `<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'; script-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'" />
    <meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no" />
    <style>
      html, body {
        margin: 0;
        padding: 0;
        background: ${safeBackground};
        overflow: hidden;
      }
      svg { display: block; width: 100%; height: auto; }
    </style>
  </head>
  <body>${svg}</body>
</html>`;
}

export function mermaidEngineDocumentLoadsRemoteScripts(html: string) {
  return /<script[^>]+src\s*=\s*["']https?:/i.test(html);
}
