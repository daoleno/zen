import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  CONNECTION_KIND_OPTIONS,
  shouldShowTelegramConnection,
  telegramSetupMode,
} from "./connectionPresentation";

const settingsSource = readFileSync(
  join(import.meta.dir, "../../app/settings.tsx"),
  "utf8",
);

function sourceBlock(start: string, end: string): string {
  const startIndex = settingsSource.indexOf(start);
  const endIndex = settingsSource.indexOf(end, startIndex);
  if (startIndex < 0 || endIndex < 0) {
    throw new Error(`Settings source block not found: ${start} -> ${end}`);
  }
  return settingsSource.slice(startIndex, endIndex);
}

describe("Settings connection information architecture", () => {
  test("the unified add flow exposes the two distinct connection roles", () => {
    expect(CONNECTION_KIND_OPTIONS).toEqual([
      {
        kind: "server",
        label: "Zen Server",
        icon: "server-outline",
        participatesInCurrentServer: true,
      },
      {
        kind: "telegram",
        label: "Telegram",
        icon: "paper-plane-outline",
        participatesInCurrentServer: false,
      },
    ]);
    expect(settingsSource).toContain(">Add Connection</Text>");
    expect(settingsSource).toContain("CONNECTION_KIND_OPTIONS.map");
  });

  test("Telegram appears as a connection only when configured or being added", () => {
    expect(shouldShowTelegramConnection(undefined, false)).toBe(false);
    expect(shouldShowTelegramConnection("", false)).toBe(false);
    expect(shouldShowTelegramConnection("zen_owner_bot", false)).toBe(true);
    expect(shouldShowTelegramConnection(undefined, true)).toBe(true);
  });

  test("Telegram setup remains enterable without a reachable current server", () => {
    expect(telegramSetupMode(undefined, false)).toBe("local");
    expect(telegramSetupMode("current-daemon", false)).toBe("local");
    expect(telegramSetupMode("current-daemon", true)).toBe("direct");

    const telegram = sourceBlock(
      "function TelegramConnectionRow",
      "function ConnectionAction",
    );
    expect(telegram).toContain("On the machine running Zen");
    expect(telegram).toContain("zen telegram setup");
    expect(telegram).toContain('label="Open BotFather"');

    const localSetup = sourceBlock(
      "const renderLocalTelegramSetup",
      "const stateLabel",
    );
    expect(localSetup).not.toContain("secureTextEntry");
    expect(localSetup).not.toContain("setToken");
    expect(localSetup).not.toContain("configureTelegramConnection");
    expect(telegram).toContain(
      'const activeServerId = setupMode === "direct" && serverId ? serverId : null',
    );
    expect(telegram).toContain("const visibleStatus = activeServerId ? status : null");
  });

  test("Zen Server selection preserves the established pairing path", () => {
    const chooser = sourceBlock(
      "const chooseConnectionKind",
      "const openEditServer",
    );
    expect(chooser).toContain('if (kind === "server")');
    expect(chooser).toContain("openCreateServer();");

    const pairing = sourceBlock(
      "const openCreateServer",
      "const chooseConnectionKind",
    );
    expect(pairing).toContain("setPairPresentation(openPairEditor())");
    expect(settingsSource).toContain("openPairScanner(current)");
    expect(settingsSource).toContain("await importServer(data || \"\")");
    expect(settingsSource).toContain("await handleImportDraft()");
    expect(settingsSource).not.toContain(
      "Advanced / Self-managed: run zen pair",
    );
  });

  test("Telegram never participates in current-server selection", () => {
    const telegram = sourceBlock(
      "function TelegramConnectionRow",
      "function ConnectionAction",
    );
    expect(telegram).not.toContain("switchCurrentServer");
    expect(telegram).not.toContain("connectServer(");
    expect(telegram).not.toMatch(/label=["{]Use/);
  });

  test("Telegram state remounts and setup requests rebind to the canonical daemon", () => {
    expect(settingsSource).toContain(
      'key={currentServerId || "no-current-server"}',
    );
    expect(settingsSource).toContain(
      "telegramOpenRequest?.serverId === currentServerId",
    );
    expect(settingsSource).toContain(
      ".getTelegramConnectionStatus(serverId)",
    );
    expect(settingsSource).toContain("if (!serverId || !connected)");
    expect(settingsSource).toContain("setStatus(null)");
  });

  test("Telegram setup is ordered, action-led, and uses official links", () => {
    const telegram = sourceBlock(
      "function TelegramConnectionRow",
      "function ConnectionAction",
    );
    expect(settingsSource).toContain(
      'const TELEGRAM_BOTFATHER_URL = "https://t.me/BotFather"',
    );
    expect(telegram).toContain("Linking.openURL(TELEGRAM_BOTFATHER_URL)");
    expect(telegram).toContain('label="Open BotFather"');
    expect(telegram).toContain(
      'accessibilityLabel="Open official BotFather chat in Telegram"',
    );
    expect(telegram).toContain("Create or select a bot");
    expect(telegram).toContain("Verify the bot token");
    expect(telegram).toContain("Connect your bot");
    expect(telegram).toContain('label="Connect Telegram"');
    expect(telegram).toContain('icon="open-outline"');
    expect(telegram).not.toContain("Bind Owner");
    expect(telegram).not.toContain("Open Telegram");
  });

  test("token input is secure, explicitly pasted, and cleared on every exit", () => {
    const telegram = sourceBlock(
      "function TelegramConnectionRow",
      "function ConnectionAction",
    );
    expect(telegram).toContain("secureTextEntry");
    expect(telegram).toContain("await Clipboard.getStringAsync()");
    expect(telegram).toContain(
      'accessibilityLabel="Paste Telegram bot token from clipboard"',
    );
    expect(telegram).toContain('setToken("")');
    expect(telegram.match(/setToken\(""\)/g)?.length).toBeGreaterThanOrEqual(5);
    expect(telegram).toContain(
      "Telegram cloud messages are not deleted.",
    );
    expect(telegram).toContain(
      "Remove the verified Telegram owner and require a new binding?",
    );
  });

  test("owner binding stays automatic and contains no manual identity fields", () => {
    const telegram = sourceBlock(
      "function TelegramConnectionRow",
      "function ConnectionAction",
    );
    expect(telegram).toContain("wsClient.beginTelegramBinding(serverId)");
    expect(telegram).toContain("Linking.openURL(challenge.url)");
    expect(telegram).toContain(
      "wsClient.getTelegramConnectionStatus(serverId)",
    );
    expect(telegram).not.toMatch(/user id|chat id/i);
    expect(telegram).not.toMatch(/manual.*(owner|telegram)/i);
  });

  test("happy-path setup omits internal architecture and retention prose", () => {
    for (const internalCopy of [
      "Bot chats are Telegram cloud chats",
      "The token remains on this daemon",
      "owner not bound",
    ]) {
      expect(settingsSource).not.toContain(internalCopy);
    }
  });

  test("connection controls expose roles, state, and disabled state accessibly", () => {
    expect(settingsSource).toContain('accessibilityLabel="Add connection"');
    expect(settingsSource).toContain("accessibilityLabel={option.label}");
    expect(settingsSource).toContain("accessibilityState={{ expanded }}");
    expect(settingsSource).toContain(
      "accessibilityState={{ disabled, busy: disabled }}",
    );
    expect(settingsSource).toContain('accessibilityLabel="Telegram bot token"');
    expect(settingsSource).toContain(
      'accessibilityLabel="Connect Telegram using the one-time binding link"',
    );
  });

  test("the rejected taxonomy is absent from Settings presentation", () => {
    const rejectedSection = ["Chan", "nels"].join("");
    const rejectedAction = ["Channel", "Action"].join("");
    const rejectedStyle = ["channel", "Action"].join("");
    expect(settingsSource).not.toMatch(
      new RegExp(`>\\s*${rejectedSection}\\s*<`),
    );
    expect(settingsSource).not.toContain(rejectedAction);
    expect(settingsSource).not.toContain(rejectedStyle);
    expect(settingsSource).not.toMatch(/>\s*Add Server\s*</);
  });
});
