import { describe, expect, test } from "bun:test";
import {
  detectMermaidDiagramKind,
  prepareMermaidDiagram,
} from "./mermaidEngine";
import { PERPETUO_SINGLE_DOMAIN_FLOWCHART, SIMPLE_FLOWCHART_LR } from "./mermaidFixtures";
import {
  mermaidSourceLooksUnsafe,
  prepareTrustedMermaidSource,
  sanitizeMermaidSvg,
  stripMermaidOverrides,
} from "./mermaidSecurity";

describe("Mermaid security and engine prepare", () => {
  test("strips init, frontmatter, and click before render", () => {
    const raw = `---
config:
  securityLevel: loose
---
%%{init: {'securityLevel': 'loose', 'flowchart': {'htmlLabels': true}}}%%
flowchart TD
  A[Safe] --> B[Still safe]
click A href "javascript:alert(1)"
click B call evil()
`;
    const prepared = prepareTrustedMermaidSource(raw);
    expect(prepared).not.toContain("securityLevel");
    expect(prepared).not.toContain("click ");
    expect(prepared.startsWith("flowchart TD")).toBe(true);
    expect(mermaidSourceLooksUnsafe(prepared)).toBe(false);
  });

  test("keeps mermaid line-break tags and drops other HTML", () => {
    const prepared = prepareTrustedMermaidSource(
      `flowchart TD\n  A["Hello<script>alert(1)</script><br/>World"]`,
    );
    expect(prepared).toContain("<br/>");
    expect(prepared).not.toContain("script");
  });

  test("rejects javascript URLs", () => {
    expect(
      mermaidSourceLooksUnsafe(`flowchart TD\n  A-->B\n  click A href "javascript:alert(1)"`),
    ).toBe(true);
  });

  test("accepts the Perpetuo flowchart and simple LR after sanitizing", () => {
    expect(detectMermaidDiagramKind(PERPETUO_SINGLE_DOMAIN_FLOWCHART)).toBe(
      "flowchart",
    );
    expect(prepareMermaidDiagram(PERPETUO_SINGLE_DOMAIN_FLOWCHART).ok).toBe(true);
    expect(prepareMermaidDiagram(SIMPLE_FLOWCHART_LR).ok).toBe(true);
  });

  test("does not render while streaming or while the fence is open", () => {
    expect(
      prepareMermaidDiagram(SIMPLE_FLOWCHART_LR, { streaming: true }).ok,
    ).toBe(false);
    expect(
      prepareMermaidDiagram(SIMPLE_FLOWCHART_LR, { closed: false }).ok,
    ).toBe(false);
  });

  test("falls back for unsupported diagram types and oversize sources", () => {
    expect(
      prepareMermaidDiagram("sequenceDiagram\n  A->>B: hi").ok,
    ).toBe(false);
    expect(
      prepareMermaidDiagram("pie title Pets\n  \"Dogs\": 386").ok,
    ).toBe(false);
    const huge = `flowchart TD\n${"  A-->B\n".repeat(4000)}`;
    expect(prepareMermaidDiagram(huge)).toMatchObject({
      ok: false,
      reason: "oversized",
    });
  });

  test("removes scripts from SVG payloads", () => {
    const svg = sanitizeMermaidSvg(
      `<svg><script>alert(1)</script><g onclick="alert(1)">ok</g></svg>`,
    );
    expect(svg).not.toContain("<script");
    expect(svg).not.toContain("onclick");
  });

  test("stripMermaidOverrides does not let later init win", () => {
    const stripped = stripMermaidOverrides(
      `%%{init: {'securityLevel':'loose'}}%%\nflowchart TD\nA-->B`,
    );
    expect(stripped).toBe("flowchart TD\nA-->B");
  });
});
