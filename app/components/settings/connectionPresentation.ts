export type TelegramSetupMode = "direct" | "local";

export function telegramSetupMode(
  serverId: string | undefined,
  connected: boolean,
): TelegramSetupMode {
  return serverId && connected ? "direct" : "local";
}
