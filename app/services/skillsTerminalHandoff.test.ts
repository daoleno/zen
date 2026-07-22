import { describe, expect, test } from "bun:test";
import type { SkillsMutationCommand } from "./skillsManagement";
import {
  createOwnedSkillsTerminalSession,
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
  test("a successful owned Session stays live and is not aborted", async () => {
    const created: string[] = [];
    const aborted: Array<[string, string]> = [];
    const result = await createOwnedSkillsTerminalSession({
      serverId: "server-a",
      createSession: async (serverId) => {
        created.push(serverId);
        return "session-new";
      },
      isCurrent: () => true,
      abortSession: (serverId, agentId) => aborted.push([serverId, agentId]),
    });

    expect(result).toEqual({ status: "created", agentId: "session-new" });
    expect(created).toEqual(["server-a"]);
    expect(aborted).toEqual([]);
  });

  test("a pre-create failure preserves its error and aborts nothing", async () => {
    const failure = new Error("create failed");
    const aborted: Array<[string, string]> = [];
    let currentChecks = 0;

    await expect(
      createOwnedSkillsTerminalSession({
        serverId: "server-a",
        createSession: async () => {
          throw failure;
        },
        isCurrent: () => {
          currentChecks += 1;
          return false;
        },
        abortSession: (serverId, agentId) => aborted.push([serverId, agentId]),
      }),
    ).rejects.toBe(failure);
    expect(currentChecks).toBe(0);
    expect(aborted).toEqual([]);
  });

  test("a post-create stale switch aborts only the fresh old-server Session once", async () => {
    const aborted: Array<[string, string]> = [];
    const result = await createOwnedSkillsTerminalSession({
      serverId: "server-old",
      createSession: async () => "session-fresh",
      isCurrent: () => false,
      abortSession: (serverId, agentId) => {
        aborted.push([serverId, agentId]);
        throw new Error("old socket already closed");
      },
    });

    expect(result).toEqual({ status: "stale", agentId: "session-fresh" });
    expect(aborted).toEqual([["server-old", "session-fresh"]]);
  });

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
    expect(
      unconfirmedSkillsTerminalHandoff(submission, "wrong-pty"),
    ).toBeNull();
    expect(unconfirmedSkillsTerminalHandoff(submission, "pty-a")).toEqual({
      kind: "not-confirmed",
      command: installCommand,
    });
  });
});
