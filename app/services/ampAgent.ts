import { isAmpCommand } from "./agentCommands";

// Presentation only: neither catalog registration nor an account/CLI probe.
export const AMP_ACCOUNT_STATUS = "CLI and Amp account: not verified";
export const AMP_CAPABILITY_SUMMARY =
  "Terminal only. No Zen Chat, tool events, native threads or resume.";
export const AMP_LAUNCH_LIMITATION =
  "Launch unavailable: Amp CLI readiness and exit handling are not verified by Zen.";
export const AMP_DELEGATION_LIMITATION =
  "Delegation unavailable: no Amp turn admission or completion reader in Zen.";

export function isAmpExecutor(executor?: {
  id: string;
  command?: string;
  provider?: string;
} | null): boolean {
  return Boolean(executor && (
    executor.id.trim().toLowerCase() === "amp" ||
    executor.provider?.trim().toLowerCase() === "amp" ||
    isAmpCommand(executor.command)
  ));
}
