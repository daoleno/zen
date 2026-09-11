import { describe, expect, test } from "bun:test";
import { parseMessageBlocks } from "../terminal/InterfaceMessageBodyModel";
import {
  isMermaidFenceLanguage,
  markdownHasMermaidFence,
  splitMarkdownMermaidSegments,
} from "./mermaidFences";
import {
  MERMAID_MARKDOWN_FIXTURE,
  PERPETUO_SINGLE_DOMAIN_FLOWCHART,
  SIMPLE_FLOWCHART_TD,
} from "./mermaidFixtures";

describe("Mermaid Markdown fences", () => {
  test("recognizes mermaid language only", () => {
    expect(isMermaidFenceLanguage("mermaid")).toBe(true);
    expect(isMermaidFenceLanguage("MERMAID")).toBe(true);
    expect(isMermaidFenceLanguage("ts")).toBe(false);
    expect(isMermaidFenceLanguage("text")).toBe(false);
  });

  test("splits the Perpetuo fixture without eating nested example fences in TypeScript", () => {
    const segments = splitMarkdownMermaidSegments(MERMAID_MARKDOWN_FIXTURE);
    const mermaid = segments.filter((segment) => segment.type === "mermaid");
    expect(mermaid).toHaveLength(2);
    expect(mermaid[0]?.closed).toBe(true);
    expect(mermaid[0]?.source).toContain("subgraph Browser");
    expect(mermaid[0]?.source).toContain("Harness");
    expect(mermaid[1]?.source).toContain("flowchart TD");
    expect(segments.some((segment) => segment.type === "markdown" && segment.text.includes("```ts"))).toBe(true);
  });

  test("keeps quoted and nested-looking fences as markdown", () => {
    const value = `> not a fence\n> \`\`\`mermaid\n> flowchart TD\n> A-->B\n> \`\`\`\n`;
    expect(markdownHasMermaidFence(value)).toBe(false);
  });

  test("marks an unfinished streaming fence as open", () => {
    const segments = splitMarkdownMermaidSegments(
      "intro\n```mermaid\nflowchart TD\n  A-->B\n",
    );
    expect(segments).toEqual([
      { type: "markdown", text: "intro" },
      { type: "mermaid", source: "flowchart TD\n  A-->B", closed: false },
    ]);
  });

  test("agrees with parseMessageBlocks for mermaid language and closure", () => {
    const markdown = `Hello\n\n\`\`\`mermaid\n${SIMPLE_FLOWCHART_TD.trim()}\n\`\`\`\n`;
    const blocks = parseMessageBlocks(markdown);
    const code = blocks.find((block) => block.type === "code");
    const mermaid = splitMarkdownMermaidSegments(markdown).find(
      (segment) => segment.type === "mermaid",
    );
    expect(code).toMatchObject({
      type: "code",
      language: "mermaid",
      closed: true,
      text: mermaid?.source,
    });
  });

  test("preserves the exact Perpetuo source through the message parser", () => {
    const markdown = `\`\`\`mermaid\n${PERPETUO_SINGLE_DOMAIN_FLOWCHART.trim()}\n\`\`\``;
    const block = parseMessageBlocks(markdown).find((item) => item.type === "code");
    expect(block?.type === "code" && block.language).toBe("mermaid");
    expect(block?.type === "code" && block.text).toBe(
      PERPETUO_SINGLE_DOMAIN_FLOWCHART.trim(),
    );
  });
});
