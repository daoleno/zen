import { supportsNativeTerminalPlatform } from "../components/terminal/terminalPlatform";

export type TerminalPlatform = "android" | "ios" | "web" | string;

export interface TerminalCapabilityPresentation {
  supported: boolean;
  title: string;
  detail: string;
  hint: string;
}

export function getTerminalCapabilityPresentation(
  platform: TerminalPlatform,
): TerminalCapabilityPresentation {
  if (supportsNativeTerminalPlatform(platform)) {
    return {
      supported: true,
      title: "Terminal available",
      detail: "This build uses the native libghostty VT core.",
      hint: "",
    };
  }

  return {
    supported: false,
    title: "Terminal unavailable on this platform",
    detail:
      "This build only ships the libghostty-backed terminal on Android and iOS.",
    hint: "Use the Android or iOS app for Terminal access.",
  };
}
