export const CONNECTION_KIND_OPTIONS = [
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
] as const;

export type ConnectionKind = (typeof CONNECTION_KIND_OPTIONS)[number]["kind"];

export function shouldShowTelegramConnection(
  botUsername: string | undefined,
  setupRequested: boolean,
): boolean {
  return Boolean(botUsername) || setupRequested;
}

export type TelegramSetupMode = "direct" | "local";

export function telegramSetupMode(
  serverId: string | undefined,
  connected: boolean,
): TelegramSetupMode {
  return serverId && connected ? "direct" : "local";
}
