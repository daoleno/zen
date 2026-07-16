import type { NativeTerminalCrashBreadcrumb } from "./nativeTerminalDiagnostics";

export interface NativeTerminalUnfinishedOperationDiagnostic {
  operation: string;
  detail: string;
  environment: string;
}

interface ConsumeUnfinishedNativeTerminalBreadcrumbOptions {
  breadcrumb: NativeTerminalCrashBreadcrumb | null;
  clearBreadcrumb(): void;
  log(
    message: string,
    diagnostic: NativeTerminalUnfinishedOperationDiagnostic,
  ): void;
}

export function consumeUnfinishedNativeTerminalBreadcrumb({
  breadcrumb,
  clearBreadcrumb,
  log,
}: ConsumeUnfinishedNativeTerminalBreadcrumbOptions): boolean {
  if (!breadcrumb || breadcrumb.stage !== "before") {
    return false;
  }

  clearBreadcrumb();

  const device = [breadcrumb.brand, breadcrumb.model]
    .filter(Boolean)
    .join(" ")
    .trim();
  const environment = [
    device,
    breadcrumb.abi,
    breadcrumb.sdkInt ? `SDK ${breadcrumb.sdkInt}` : "",
  ]
    .filter(Boolean)
    .join(" / ");

  log("[native-terminal] consumed unfinished operation breadcrumb", {
    operation: breadcrumb.operation || "unknown",
    detail: breadcrumb.detail || "none",
    environment: environment || "unknown",
  });
  return true;
}
