import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, test } from "bun:test";
import { parseMessageBlocks } from "../terminal/InterfaceMessageBodyModel";
import {
  pngIsNonBlank,
  renderMermaidSvgWithChrome,
} from "./mermaidChromeRender";
import {
  PERPETUO_SINGLE_DOMAIN_FLOWCHART,
  SIMPLE_FLOWCHART_LR,
  SIMPLE_FLOWCHART_TD,
} from "./mermaidFixtures";

const shotDir = join(process.env.TMPDIR || "/tmp", "zen-mermaid-shots");
mkdirSync(shotDir, { recursive: true });

describe("Mermaid actual render", () => {
  test("renders the Perpetuo subgraphs, Chinese labels, numbered and dotted edges", async () => {
    const shot = join(shotDir, "perpetuo-390.png");
    const result = await renderMermaidSvgWithChrome(
      PERPETUO_SINGLE_DOMAIN_FLOWCHART,
      { screenshotPath: shot, width: 390, height: 844 },
    );
    expect(result.svg.includes("<svg")).toBe(true);
    expect(result.width).toBeGreaterThan(40);
    expect(result.height).toBeGreaterThan(40);
    for (const label of [
      "用户浏览器",
      "可信主界面",
      "应用文档组装器",
      "隔离构建容器",
      "持久文件存储",
    ]) {
      expect(result.svg).toContain(label);
    }
    for (const node of [
      "flowchart-Shell-",
      "flowchart-Frame-",
      "flowchart-Server-",
      "flowchart-Compose-",
      "flowchart-Proxy-",
      "flowchart-Harness-",
      "flowchart-Build-",
      "flowchart-Files-",
      "flowchart-DB-",
    ]) {
      expect(result.svg).toContain(node);
    }
    expect(result.svg).toContain('id="Browser"');
    expect(result.svg).toContain("Perpetuo");
    expect(result.svg).toContain("edge-pattern-dotted");
    expect(result.svg).toContain("⑤");
    expect(pngIsNonBlank(shot)).toBe(true);
  }, 20_000);

  test("renders simple TD and LR flowcharts", async () => {
    const td = await renderMermaidSvgWithChrome(SIMPLE_FLOWCHART_TD);
    const lr = await renderMermaidSvgWithChrome(SIMPLE_FLOWCHART_LR, {
      width: 1280,
      height: 720,
      screenshotPath: join(shotDir, "simple-lr-wide.png"),
    });
    expect(td.svg).toContain("开始");
    expect(td.svg).toContain("完成");
    expect(lr.svg).toContain("Input");
    expect(lr.svg).toContain("Output");
    expect(pngIsNonBlank(join(shotDir, "simple-lr-wide.png"))).toBe(true);
  }, 20_000);

  test("renders dark and light without going blank", async () => {
    const dark = await renderMermaidSvgWithChrome(SIMPLE_FLOWCHART_TD, {
      dark: true,
      screenshotPath: join(shotDir, "simple-td-dark.png"),
    });
    const light = await renderMermaidSvgWithChrome(SIMPLE_FLOWCHART_TD, {
      dark: false,
      screenshotPath: join(shotDir, "simple-td-light.png"),
    });
    expect(dark.svg).toContain("<svg");
    expect(light.svg).toContain("<svg");
    expect(pngIsNonBlank(join(shotDir, "simple-td-dark.png"))).toBe(true);
    expect(pngIsNonBlank(join(shotDir, "simple-td-light.png"))).toBe(true);
  }, 20_000);

  test("message parser still exposes mermaid source for copy after render", () => {
    const markdown = `\`\`\`mermaid\n${PERPETUO_SINGLE_DOMAIN_FLOWCHART.trim()}\n\`\`\``;
    const block = parseMessageBlocks(markdown).find((item) => item.type === "code");
    expect(block?.type === "code" && block.text).toBe(
      PERPETUO_SINGLE_DOMAIN_FLOWCHART.trim(),
    );
  });
});
