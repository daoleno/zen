// Fixed public CLI arguments only. This boundary never accepts account or provider data.
const AMP_EXTERNAL_ARGUMENTS = Object.freeze([
  "amp",
  "--visibility", "private",
  "--no-ide",
  "--no-remote-control-terminal",
] as const);

export const AMP_EXTERNAL_COMMAND = AMP_EXTERNAL_ARGUMENTS.join(" ");

export type AmpHandoffCapability = {
  kind: "external-command";
  account: "unverified";
  launch: "unavailable";
  reason: "external-terminal-required";
  command: string;
};

export function getAmpHandoffCapability(): AmpHandoffCapability {
  // Neither device platform nor the Zen provider catalog proves an Amp CLI/login.
  return {
    kind: "external-command",
    account: "unverified",
    launch: "unavailable",
    reason: "external-terminal-required",
    command: AMP_EXTERNAL_COMMAND,
  };
}

export type AmpHandoffResult =
  | { kind: "copied" }
  | { kind: "unavailable"; reason: "clipboard-unavailable" };

export async function copyAmpHandoff(
  writeClipboard: (command: string) => Promise<boolean>,
): Promise<AmpHandoffResult> {
  try {
    if (await writeClipboard(AMP_EXTERNAL_COMMAND) === true) {
      return { kind: "copied" };
    }
  } catch {
    // Native/browser errors may include private context. Never retain or log them.
  }
  return { kind: "unavailable", reason: "clipboard-unavailable" };
}
