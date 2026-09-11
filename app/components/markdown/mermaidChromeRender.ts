import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { parseMermaidSvgSize } from "./mermaidSvgLayout";
import { MERMAID_ENGINE_BOOTSTRAP } from "./mermaidWebViewHtml";

export type ChromeMermaidRender = {
  svg: string;
  width: number;
  height: number;
  screenshotPath?: string;
};

function mermaidMinPath() {
  return join(import.meta.dir, "../../../node_modules/mermaid/dist/mermaid.min.js");
}

export function mermaidMinSha256() {
  return createHash("sha256").update(readFileSync(mermaidMinPath())).digest("hex");
}

export async function renderMermaidSvgWithChrome(
  source: string,
  options: {
    screenshotPath?: string;
    width?: number;
    height?: number;
    dark?: boolean;
  } = {},
): Promise<ChromeMermaidRender> {
  const dir = join(process.env.TMPDIR || "/tmp", `zen-mermaid-${Date.now()}-${Math.random().toString(16).slice(2)}`);
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "mermaid.min.js"), readFileSync(mermaidMinPath()));
  const html = `<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <style>
      html, body { margin: 0; padding: 12px; background: ${options.dark ? "#111" : "#fff"}; }
      #diagram svg { max-width: 100%; height: auto; display: block; }
    </style>
  </head>
  <body>
    <div id="diagram"></div>
    <script src="./mermaid.min.js"></script>
    <script>
      window.ReactNativeWebView = {
        postMessage: function (raw) {
          var payload = JSON.parse(raw);
          if (payload.type === "ready") return;
          window.__zenResult = payload;
          if (payload.ok && payload.svg) {
            document.getElementById("diagram").innerHTML = payload.svg;
            document.documentElement.setAttribute("data-done", "ok");
            document.documentElement.setAttribute("data-bytes", String(payload.svg.length));
          } else {
            document.documentElement.setAttribute("data-done", "err");
            document.documentElement.setAttribute("data-error", payload.error || "render");
          }
        }
      };
${MERMAID_ENGINE_BOOTSTRAP}
      window.__zenMermaidRender({
        v: 1,
        type: "render",
        requestId: "chrome",
        generation: 1,
        source: ${JSON.stringify(source)},
        theme: {
          mode: ${JSON.stringify(options.dark ? "dark" : "light")},
          primaryColor: ${JSON.stringify(options.dark ? "#222" : "#f7f7f7")},
          primaryTextColor: ${JSON.stringify(options.dark ? "#eee" : "#111")},
          primaryBorderColor: "#888",
          lineColor: "#888",
          secondaryColor: ${JSON.stringify(options.dark ? "#333" : "#eee")},
          tertiaryColor: ${JSON.stringify(options.dark ? "#2a2a2a" : "#e8e8e8")},
          clusterBkg: ${JSON.stringify(options.dark ? "#1c1c1c" : "#f2f2f2")},
          clusterBorder: "#888",
          edgeLabelBackground: ${JSON.stringify(options.dark ? "#111" : "#fff")},
          fontSize: "13px"
        },
        timeoutMs: 2500
      });
    </script>
  </body>
</html>`;
  writeFileSync(join(dir, "index.html"), html);
  const chrome = process.env.CHROME_PATH || "google-chrome";
  const args = [
    "--headless=new",
    "--disable-gpu",
    "--no-sandbox",
    "--allow-file-access-from-files",
    "--virtual-time-budget=8000",
    `--window-size=${options.width ?? 390},${options.height ?? 844}`,
    "--dump-dom",
  ];
  if (options.screenshotPath) {
    args.push(`--screenshot=${options.screenshotPath}`);
  }
  args.push(`file://${join(dir, "index.html")}`);
  const dumped = Bun.spawnSync([chrome, ...args], {
    stdout: "pipe",
    stderr: "pipe",
  });
  if (dumped.exitCode !== 0) {
    throw new Error(
      `chrome mermaid render failed: ${new TextDecoder().decode(dumped.stderr).slice(0, 500)}`,
    );
  }
  const dom = new TextDecoder().decode(dumped.stdout);
  const done = /data-done="([^"]+)"/.exec(dom)?.[1];
  if (done !== "ok") {
    const error = /data-error="([^"]*)"/.exec(dom)?.[1] || "blank";
    throw new Error(`mermaid chrome render: ${error}`);
  }
  const svgMatch = /<div id="diagram">([\s\S]*?)<\/div>/.exec(dom);
  const svg = svgMatch?.[1]?.trim() || "";
  if (!svg.includes("<svg")) {
    throw new Error("mermaid chrome render produced no svg");
  }
  const size = parseMermaidSvgSize(svg, { width: 320, height: 180 });
  return { svg, width: size.width, height: size.height, screenshotPath: options.screenshotPath };
}

export function pngIsNonBlank(path: string) {
  const bytes = readFileSync(path);
  if (bytes.length < 64 || bytes[0] !== 0x89 || bytes[1] !== 0x50) {
    return false;
  }
  const unique = new Set(bytes.subarray(0, Math.min(bytes.length, 4096)));
  return unique.size > 8;
}
