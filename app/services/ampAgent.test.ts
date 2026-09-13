import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { AMP_COMMAND, isAmpCommand, PI_COMMAND, OPENCODE_COMMAND, SUPPORTED_AGENT_TARGETS } from "./agentCommands";
import { AMP_ACCOUNT_STATUS, AMP_DELEGATION_LIMITATION, isAmpExecutor } from "./ampAgent";
import { presentWorker } from "./workerPresentation";
import { brainAdapterLabel, brainAdapterProviderKey, brainExecutorOptions } from "../components/brain/brainPresentation";

describe("Amp peer presentation boundary", () => {
  test("recognizes exact direct CLI identity, not unrelated tools or prompt text", () => {
    for (const command of ["amp", AMP_COMMAND, "/opt/bin/amp --visibility private", "'/opt/bin/amp' --no-ide"]) {
      expect(isAmpCommand(command)).toBe(true);
    }
    for (const command of ["amplify", "example", "echo amp", "codex amp", "", undefined]) {
      expect(isAmpCommand(command)).toBe(false);
    }
    expect(isAmpExecutor({ id: "reviewer", command: AMP_COMMAND })).toBe(true);
    expect(isAmpExecutor({ id: "amp" })).toBe(true);
    expect(isAmpExecutor({ id: "example" })).toBe(false);
  });

  test("projects a disabled peer without registering a command, provider or capabilities", () => {
    const peers = ["codex", "pi", "grok", "opencode"].map((id) => ({ id, name: id }));
    const options = brainExecutorOptions(peers);
    expect(options.slice(0, 4)).toEqual(peers);
    expect(options[4]).toEqual({ id: "amp", name: "Amp", unavailableReason: AMP_DELEGATION_LIMITATION });
    expect(peers).toHaveLength(4);
    expect(brainAdapterLabel(options[4])).toBe("Amp");
    expect(brainAdapterProviderKey(options[4])).toBe("amp");
  });

  test("configured Amp aliases stay disabled without duplicates or stale server state", () => {
    const amp = { id: "reviewer", name: "Amp review", command: AMP_COMMAND, provider: "custom", capabilities: { interactive_tty: true } };
    const first = brainExecutorOptions([amp]);
    expect(first).toEqual([{ ...amp, unavailableReason: AMP_DELEGATION_LIMITATION }]);
    expect(brainExecutorOptions([])).toEqual([{ id: "amp", name: "Amp", unavailableReason: AMP_DELEGATION_LIMITATION }]);
    expect(amp).not.toHaveProperty("unavailableReason");
    expect(AMP_ACCOUNT_STATUS).toBe("CLI and Amp account: not verified");
  });

  test("existing terminals get Amp identity, not inferred chat or billing readiness", () => {
    const worker = presentWorker({ name: "amp", command: AMP_COMMAND, cwd: "/repo", summary: "", last_output_lines: [] });
    expect(worker.kind).toBe("amp");
    expect(worker.typeLabel).toBe("Amp (Terminal)");
    expect(worker.title).toBe("repo");
    expect(PI_COMMAND).toBe("pi");
    expect(OPENCODE_COMMAND).toBe("opencode");
    expect(SUPPORTED_AGENT_TARGETS.some((target) => (target.id as string) === "amp")).toBe(false);
  });

  test("removes Settings handoff and keeps availability metadata free of transport and credential access", () => {
    const settings = readFileSync(new URL("../app/settings.tsx", import.meta.url), "utf8");
    expect(settings).not.toMatch(/AmpHandoff|External agents|ampHandoff/);
    const metadata = readFileSync(new URL("./ampAgent.ts", import.meta.url), "utf8");
    expect(metadata).not.toMatch(/wsClient|fetch\(|Clipboard|readFile|process\.env|auth\.json|api_key|apiKey|console\./);
  });
});
