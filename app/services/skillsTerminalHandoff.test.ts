import { describe, expect, test } from "bun:test";
import type { SkillsMutationCommand } from "./skillsManagement";
import {
  SkillsTerminalHandoffOwner,
  submitSkillsTerminalHandoff,
  unconfirmedSkillsTerminalHandoff,
} from "./skillsTerminalHandoff";

const installCommand: SkillsMutationCommand = {
  operation: "install",
  command:
    "npx skills add https://github.com/acme/skills --skill useful --global --agent codex --yes",
  catalogId: "acme/skills/useful",
  source: "acme/skills",
  skillName: "useful",
  scope: "global",
  agents: ["codex"],
};

describe("Skills Terminal handoff", () => {
  test("the exact command can be claimed once only by its created Session", () => {
    const owner = new SkillsTerminalHandoffOwner();
    const token = owner.issue("server-a:session-a", installCommand);

    expect(owner.claim("server-a:other-session", token)).toBeNull();
    expect(owner.claim("server-a:session-a", "wrong-token")).toBeNull();
    expect(owner.claim("server-a:session-a", token)).toEqual({
      input: `${installCommand.command}\r`,
      command: installCommand,
    });
    expect(owner.claim("server-a:session-a", token)).toBeNull();
  });

  test("a newer handoff invalidates the older grant and clear prevents replay", () => {
    const owner = new SkillsTerminalHandoffOwner();
    const first = owner.issue("server-a:session-a", installCommand);
    const second = owner.issue("server-a:session-b", installCommand);

    expect(owner.claim("server-a:session-a", first)).toBeNull();
    owner.clear();
    expect(owner.claim("server-a:session-b", second)).toBeNull();
  });

  test("revoking an abandoned route removes only its matching grant", () => {
    const owner = new SkillsTerminalHandoffOwner();
    const token = owner.issue("server-a:session-a", installCommand);

    owner.revoke("server-a:other-session", token);
    expect(owner.claim("server-a:session-a", token)?.input).toBe(
      `${installCommand.command}\r`,
    );
  });

  test("synchronous submission failure is visible and never replayed", () => {
    const owner = new SkillsTerminalHandoffOwner();
    const token = owner.issue("server-a:session-a", installCommand);
    const failures: unknown[] = [];
    const submission = submitSkillsTerminalHandoff(
      owner,
      "server-a:session-a",
      token,
      "pty-a",
      () => {
        throw new Error("socket closed");
      },
      (failure) => failures.push(failure),
    );
    expect(submission).toBeNull();
    expect(failures).toEqual([
      { kind: "not-submitted", command: installCommand },
    ]);
    expect(owner.claim("server-a:session-a", token)).toBeNull();
  });

  test("success submits exactly once across rerender, renderer reload, and reconnect", () => {
    const owner = new SkillsTerminalHandoffOwner();
    const token = owner.issue("server-a:session-a", installCommand);
    const sent: string[] = [];
    const submit = () =>
      submitSkillsTerminalHandoff(
        owner,
        "server-a:session-a",
        token,
        "pty-a",
        (input) => sent.push(input),
        () => undefined,
      );
    const submission = submit();
    expect(submit()).toBeNull();
    expect(submit()).toBeNull();
    expect(sent).toEqual([`${installCommand.command}\r`]);
    expect(unconfirmedSkillsTerminalHandoff(submission, "wrong-pty")).toBeNull();
    expect(unconfirmedSkillsTerminalHandoff(submission, "pty-a")).toEqual({
      kind: "not-confirmed",
      command: installCommand,
    });
  });
});
