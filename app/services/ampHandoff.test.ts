import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { AMP_EXTERNAL_COMMAND, copyAmpHandoff, getAmpHandoffCapability } from "./ampHandoff";

const documentedCommand = "amp --visibility private --no-ide --no-remote-control-terminal";

test("handoff is a fixed documented command, never proof of an account or route", () => {
  expect(AMP_EXTERNAL_COMMAND).toBe(documentedCommand);
  expect(readFileSync(new URL("../../docs/amp.md", import.meta.url), "utf8")).toContain(documentedCommand);
  expect(getAmpHandoffCapability()).toEqual({
    kind: "external-command", account: "unverified", launch: "unavailable",
    reason: "external-terminal-required", command: documentedCommand,
  });
  const state = getAmpHandoffCapability();
  state.command = "untrusted replacement";
  expect(getAmpHandoffCapability().command).toBe(documentedCommand);
});

test("clipboard receives only the fixed command and success requires confirmation", async () => {
  const writes: string[] = [];
  const result = await copyAmpHandoff(async (value) => { writes.push(value); return true; });
  expect(writes).toEqual([documentedCommand]);
  expect(result).toEqual({ kind: "copied" });
  expect(await copyAmpHandoff(async () => false)).toEqual({ kind: "unavailable", reason: "clipboard-unavailable" });
  // A malformed native bridge response is not confirmation.
  expect(await copyAmpHandoff(async () => undefined as unknown as boolean)).toEqual({ kind: "unavailable", reason: "clipboard-unavailable" });
});

test("native errors and unexpected replies cannot leak sensitive context", async () => {
  const privateContext = "fixture-private-value-never-render";
  const errors = [new Error(privateContext), { message: privateContext, api_key: privateContext }, privateContext];
  for (const error of errors) {
    const result = await copyAmpHandoff(() => { throw error; });
    expect(result).toEqual({ kind: "unavailable", reason: "clipboard-unavailable" });
    expect(JSON.stringify(result)).not.toContain(privateContext);
  }
  const hostile = { toString() { throw new Error("Must not inspect failure context"); } };
  expect(await copyAmpHandoff(async () => { throw hostile; })).toEqual({ kind: "unavailable", reason: "clipboard-unavailable" });
  expect(await copyAmpHandoff(async () => ({ api_key: privateContext }) as unknown as boolean))
    .toEqual({ kind: "unavailable", reason: "clipboard-unavailable" });
});

test("settings mounts the external entry separately from existing providers and resets it on server changes", () => {
  const settings = readFileSync(new URL("../app/settings.tsx", import.meta.url), "utf8");
  expect(settings).toContain('router.push("/model-profiles")');
  expect(settings).toContain("Models and accounts");
  expect(settings).toContain("External agents");
  expect(settings).toContain('<AmpHandoffEntry key={currentServerId || "no-current-server"} />');
});

test("Amp UI and service have no credential, network, executor or provider mutation imports", () => {
  const sources = [
    new URL("./ampHandoff.ts", import.meta.url),
    new URL("../components/terminal/AmpHandoffEntry.tsx", import.meta.url),
  ].map((url) => readFileSync(url, "utf8")).join("\n");
  expect(sources).not.toMatch(/wsClient|fetch\(|openURL|TextInput|console\.|api_key|apiKey|credential_hint|auth\.json|process\.env|setDelegatedExecutor|createSession/);
});
