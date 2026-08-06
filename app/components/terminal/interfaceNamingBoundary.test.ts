import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const componentDir = import.meta.dir;

describe("Interface provider-neutral naming boundary", () => {
  test("names the shared mobile ownership chain for Interface", () => {
    const sharedOwners = [
      "InterfaceChatSurface.tsx",
      "useInterfaceChatSurfaceState.ts",
      "InterfaceChatSession.ts",
      "InterfaceChatController.ts",
      "useInterfaceMessageTransport.ts",
      "InterfaceChatBody.tsx",
      "InterfaceTimelineView.tsx",
      "InterfaceTimelineMessage.tsx",
      "InterfaceNativeMarkdownBody.tsx",
      "InterfaceChatComposer.tsx",
    ];

    for (const file of sharedOwners) {
      expect(existsSync(join(componentDir, file))).toBe(true);
    }
    expect(existsSync(join(componentDir, "CodexChatSurface.tsx"))).toBe(false);
    expect(existsSync(join(componentDir, "CodexTimelineView.tsx"))).toBe(false);
    expect(existsSync(join(componentDir, "CodexNativeMarkdownBody.tsx"))).toBe(
      false,
    );
  });

  test("keeps Codex naming only at genuine Codex feature adapters", () => {
    const codexNamedFiles = readdirSync(componentDir)
      .filter((file) => /^(?:Codex|codex|useCodex)/.test(file))
      .sort();

    expect(codexNamedFiles).toEqual([
      "CodexHeartbeatWake.ts",
      "CodexQuickCommandRow.tsx",
      "CodexSkillsSheet.tsx",
      "CodexSlashCommands.ts",
      "CodexStatusSheet.tsx",
      "codexSlashCommandPresentation.ts",
      "useCodexSlashCommandDialogs.ts",
      "useCodexSlashCommandPicker.ts",
      "useCodexSlashCommandRouter.ts",
    ]);
  });

  test("uses Interface labels and a neutral shared color owner", () => {
    const terminalViewport = readFileSync(
      join(componentDir, "TerminalViewport.tsx"),
      "utf8",
    );
    const sessionActions = readFileSync(
      join(componentDir, "screen/useTerminalSessionActions.ts"),
      "utf8",
    );

    expect(terminalViewport).toContain("interface-chat:${sessionKey}");
    expect(terminalViewport).not.toContain("codex-chat:${sessionKey}");
    expect(sessionActions).toContain("Failed to persist Interface render mode");
    expect(sessionActions).not.toContain("Failed to persist Codex render mode");
    expect(existsSync(join(componentDir, "colorWithAlpha.ts"))).toBe(true);
    expect(existsSync(join(componentDir, "gitDiffColor.ts"))).toBe(false);
  });
});
