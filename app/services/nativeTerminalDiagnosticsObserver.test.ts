import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { consumeUnfinishedNativeTerminalBreadcrumb } from "./nativeTerminalDiagnosticsObserver";

const layoutSource = readFileSync(
  join(import.meta.dir, "../app/_layout.tsx"),
  "utf8",
);
const helperSource = readFileSync(
  join(import.meta.dir, "nativeTerminalDiagnosticsObserver.ts"),
  "utf8",
);

describe("NativeTerminalDiagnosticsObserver", () => {
  test("clears a stage-before breadcrumb before emitting a local diagnostic", () => {
    const events: string[] = [];
    const breadcrumb = {
      stage: "before" as const,
      operation: "createTerminal",
      detail: "cols=80 rows=24",
      timestampMs: 1_752_600_000_000,
      abi: "x86_64",
      model: "sdk_gphone64_x86_64",
      brand: "google",
      sdkInt: 36,
      accessToken: "must-not-be-logged",
    };
    const logs: unknown[][] = [];

    const consumed = consumeUnfinishedNativeTerminalBreadcrumb({
      breadcrumb,
      clearBreadcrumb: () => {
        events.push("clear");
      },
      log: (message, diagnostic) => {
        events.push("log");
        logs.push([message, diagnostic]);
      },
    });

    expect(consumed).toBe(true);
    expect(events).toEqual(["clear", "log"]);
    expect(logs).toEqual([
      [
        "[native-terminal] consumed unfinished operation breadcrumb",
        {
          operation: "createTerminal",
          detail: "cols=80 rows=24",
          environment: "google sdk_gphone64_x86_64 / x86_64 / SDK 36",
        },
      ],
    ]);
    expect(JSON.stringify(logs)).not.toContain("must-not-be-logged");
  });

  test("ignores a completed breadcrumb without clearing or logging", () => {
    let clearCount = 0;
    let logCount = 0;

    const consumed = consumeUnfinishedNativeTerminalBreadcrumb({
      breadcrumb: {
        stage: "after",
        operation: "resize",
        detail: "cols=80 rows=24",
        timestampMs: 1_752_600_000_000,
        abi: "x86_64",
        model: "sdk_gphone64_x86_64",
        brand: "google",
        sdkInt: 36,
      },
      clearBreadcrumb: () => {
        clearCount += 1;
      },
      log: () => {
        logCount += 1;
      },
    });

    expect(consumed).toBe(false);
    expect(clearCount).toBe(0);
    expect(logCount).toBe(0);
  });

  test("has no modal path and uses the nonblocking helper", () => {
    const observerStart = layoutSource.indexOf(
      "const NativeTerminalDiagnosticsObserver",
    );
    const observerEnd = layoutSource.indexOf(
      "interface ConnectionLifecycleProps",
      observerStart,
    );
    const observerSource = layoutSource.slice(observerStart, observerEnd);
    const reactNativeImport = layoutSource.match(
      /import \{[\s\S]*?\} from "react-native";/,
    )?.[0];

    expect(observerStart).toBeGreaterThan(-1);
    expect(observerEnd).toBeGreaterThan(observerStart);
    expect(observerSource).toContain(
      "consumeUnfinishedNativeTerminalBreadcrumb({",
    );
    expect(observerSource).toContain("console.log(message, diagnostic);");
    expect(observerSource).not.toContain("Alert.alert");
    expect(observerSource).not.toContain("Native terminal crashed last run");
    expect(reactNativeImport).toBeDefined();
    expect(reactNativeImport).not.toContain("Alert");
    expect(helperSource).not.toContain("react-native");
    expect(helperSource).not.toContain("Alert");
    expect(helperSource).not.toContain("console.warn");
    expect(helperSource).not.toContain("console.error");
  });
});
