import type { SkillsMutationCommand } from "./skillsManagement";

export interface SkillsHandoffFailure {
  kind: "not-submitted" | "not-confirmed";
  command: SkillsMutationCommand;
}

export interface SkillsTerminalSubmission {
  sessionId: string;
  command: SkillsMutationCommand;
}

export class SkillsTerminalHandoffOwner {
  private current: {
    sessionKey: string;
    token: string;
    input: string;
    command: SkillsMutationCommand;
  } | null = null;
  private sequence = 0;

  issue(sessionKey: string, command: SkillsMutationCommand): string {
    if (!sessionKey || !command.command) {
      throw new Error("Cannot create an empty Terminal handoff.");
    }
    this.sequence += 1;
    const token = `skills_${Date.now().toString(36)}_${this.sequence.toString(36)}`;
    this.current = {
      sessionKey,
      token,
      input: `${command.command}\r`,
      command,
    };
    return token;
  }

  claim(sessionKey: string, token: string): {
    input: string;
    command: SkillsMutationCommand;
  } | null {
    const current = this.current;
    if (!current || current.sessionKey !== sessionKey || current.token !== token) {
      return null;
    }
    this.current = null;
    return { input: current.input, command: current.command };
  }

  revoke(sessionKey: string, token: string): void {
    if (this.current?.sessionKey === sessionKey && this.current.token === token) {
      this.current = null;
    }
  }

  clear(): void {
    this.current = null;
  }
}

export function submitSkillsTerminalHandoff(
  owner: SkillsTerminalHandoffOwner,
  sessionKey: string,
  token: string,
  sessionId: string,
  send: (input: string) => void,
  onFailure: (failure: SkillsHandoffFailure) => void,
): SkillsTerminalSubmission | null {
  const handoff = owner.claim(sessionKey, token);
  if (!handoff) return null;
  try {
    send(handoff.input);
    return { sessionId, command: handoff.command };
  } catch {
    onFailure({ kind: "not-submitted", command: handoff.command });
    return null;
  }
}

export function unconfirmedSkillsTerminalHandoff(
  submission: SkillsTerminalSubmission | null,
  sessionId?: string | null,
): SkillsHandoffFailure | null {
  if (!submission || (sessionId && submission.sessionId !== sessionId)) {
    return null;
  }
  return { kind: "not-confirmed", command: submission.command };
}

export const skillsTerminalHandoff = new SkillsTerminalHandoffOwner();
