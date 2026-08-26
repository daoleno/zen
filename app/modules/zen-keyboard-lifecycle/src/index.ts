import { requireOptionalNativeModule } from "expo-modules-core";

export interface ZenKeyboardForegroundSnapshot {
  revision: number;
  imeVisible: boolean;
  imeHeight: number;
  composerFocused: boolean;
  evidence: string;
}

interface ZenKeyboardLifecycleNativeModule {
  getForegroundSnapshot(
    composerNativeId: string,
    revision: number,
  ): Promise<ZenKeyboardForegroundSnapshot>;
}

const nativeModule =
  requireOptionalNativeModule<ZenKeyboardLifecycleNativeModule>(
    "ZenKeyboardLifecycle",
  );

export function getZenKeyboardForegroundSnapshot(
  composerNativeId: string,
  revision: number,
) {
  if (!nativeModule) {
    return Promise.resolve({
      revision,
      imeVisible: false,
      imeHeight: 0,
      composerFocused: false,
      evidence: "native_module_unavailable",
    });
  }
  return nativeModule.getForegroundSnapshot(composerNativeId, revision);
}
