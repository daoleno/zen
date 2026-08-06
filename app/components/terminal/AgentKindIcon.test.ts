import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const iconSource = readFileSync(join(import.meta.dir, "AgentKindIcon.tsx"), "utf8");
const piMarkSource = readFileSync(
  join(import.meta.dir, "../icons/PiMark.tsx"),
  "utf8",
);
const piNoticeSource = readFileSync(
  join(import.meta.dir, "../../assets/notices/PI-MARK-MIT.txt"),
  "utf8",
);

function renderContentBlock(source: string): string {
  const start = source.indexOf("function renderContent(");
  const end = source.indexOf("\nfunction renderFlavorIcon", start);
  if (start < 0 || end < 0) {
    throw new Error("renderContent block not found");
  }
  return source.slice(start, end);
}

describe("AgentKindIcon OpenCode and Pi brand marks", () => {
  test("uses LobeHub OpenCode and local PiMark on the shared component path", () => {
    expect(iconSource).toContain("import { Claude, Codex, Grok, OpenCode } from '@lobehub/icons-rn'");
    expect(iconSource).toContain("import { PiMark } from '../icons/PiMark'");
    expect(iconSource).toContain("variant?: 'compact' | 'avatar'");

    const renderContent = renderContentBlock(iconSource);
    expect(renderContent).toContain("kind === 'opencode'");
    expect(renderContent).toContain("<OpenCode size={iconSize} color={theme.isLight ? '#000' : '#fff'} />");
    expect(renderContent).toContain("kind === 'pi'");
    expect(renderContent).toContain("<PiMark size={iconSize} color={theme.isLight ? '#000' : '#fff'} />");
    expect(renderContent).not.toMatch(/letter=["']O["']/);
    expect(renderContent).not.toMatch(/letter=["']π["']/);
    expect(renderContent).not.toContain('backgroundColor="#F5A524"');
    expect(renderContent).not.toContain('backgroundColor="#1F6F5F"');
  });

  test("compact and avatar variants share the same OpenCode and Pi brand owners", () => {
    expect(iconSource).toContain("variant === 'avatar'");
    expect(iconSource).toContain("renderContent({");
    expect(iconSource).toMatch(
      /kind === 'claude' \|\| kind === 'codex' \|\| kind === 'cursor' \|\| kind === 'grok' \|\| kind === 'pi' \|\| kind === 'opencode'/,
    );
  });

  test("PiMark preserves official open-source geometry and provenance", () => {
    expect(piMarkSource).toContain('viewBox="0 0 24 24"');
    expect(piMarkSource).toContain("M1 1h16.5v11H12v5.5H6.5V23H1V1zm5.5 5.5V12H12V6.5H6.5z");
    expect(piMarkSource).toContain("M17.5 12H23v11h-5.5V12z");
    expect(piMarkSource).toContain("pi.dev/logo.svg");
    expect(piMarkSource).toContain("lobe-icons");
    expect(piNoticeSource).toContain("MIT License");
    expect(piNoticeSource).toContain("badlogic/pi-mono");
    expect(piNoticeSource).toContain("LobeHub");
  });
});
