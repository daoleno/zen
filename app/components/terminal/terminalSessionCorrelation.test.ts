import { describe, expect, test } from "bun:test";
import {
  notifyTmuxClientFocus,
  TerminalScrollCorrelation,
  TerminalSessionCorrelation,
} from "./terminalSessionCorrelation";

describe("Terminal live-session correlation", () => {
  test("input and scrolling require the exact current connected session", () => {
    const correlation = new TerminalSessionCorrelation();

    expect(correlation.canInteract).toBe(false);
    expect(correlation.acceptEvent("pre-open")).toBe(false);
    expect(correlation.beginOpen()).toBe(true);
    expect(correlation.acceptOpened("session-a")).toBe(true);
    expect(correlation.canInteract).toBe(true);
    expect(correlation.acceptEvent("session-a")).toBe(true);
    expect(correlation.acceptEvent("wrong-session")).toBe(false);
  });

  test("disconnect and replacement drop stale activity with no queue or replay", () => {
    const correlation = new TerminalSessionCorrelation();
    correlation.beginOpen();
    correlation.acceptOpened("session-a");

    correlation.disconnect();
    expect(correlation.canInteract).toBe(false);
    expect(correlation.acceptEvent("session-a")).toBe(false);

    correlation.connect();
    expect(correlation.beginOpen()).toBe(true);
    expect(correlation.acceptOpened("session-b")).toBe(true);
    expect(correlation.acceptEvent("session-a")).toBe(false);
    expect(correlation.acceptEvent("session-b")).toBe(true);
    expect(correlation.canInteract).toBe(true);
  });

  test("an opened blank screen is immediately usable without presentation acknowledgement", () => {
    const correlation = new TerminalSessionCorrelation();
    correlation.beginOpen();

    expect(correlation.acceptOpened("blank-session")).toBe(true);
    expect(correlation.canInteract).toBe(true);
    expect(correlation.sessionId).toBe("blank-session");
  });

  test("native tmux focus is one standard client-activity write with no fallback owner", () => {
    const writes: string[] = [];
    const sendInput = (data: string) => {
      writes.push(data);
      return true;
    };

    expect(notifyTmuxClientFocus("tmux", sendInput)).toBe(true);
    expect(writes).toEqual(["\u001b[I"]);
    expect(notifyTmuxClientFocus("shell", sendInput)).toBe(false);
    expect(writes).toEqual(["\u001b[I"]);
  });

  test("cancellation and session replacement invalidate already-posted scroll batches", () => {
    const scroll = new TerminalScrollCorrelation();
    const first = scroll.replace("session-a");
    expect(scroll.accept(first.sessionId, first.token)).toBe(true);

    const afterInput = scroll.replace("session-a");
    expect(scroll.accept(first.sessionId, first.token)).toBe(false);
    expect(scroll.accept(afterInput.sessionId, afterInput.token)).toBe(true);

    scroll.replace(null);
    expect(scroll.accept(afterInput.sessionId, afterInput.token)).toBe(false);

    const replacement = scroll.replace("session-b");
    expect(scroll.accept("session-a", afterInput.token)).toBe(false);
    expect(scroll.accept(replacement.sessionId, replacement.token)).toBe(true);
  });
});
