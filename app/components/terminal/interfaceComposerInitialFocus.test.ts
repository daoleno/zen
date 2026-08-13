import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  consumeInterfaceComposerInitialFocusGrant,
  isInterfaceComposerInitialFocusRouteGrant,
  reconcileInterfaceComposerInitialFocusGrant,
  resolveInterfaceComposerInitialFocusEffect,
} from "./interfaceComposerInitialFocus";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

describe("Interface Composer initial focus", () => {
  test("an existing or revisited Session never creates a focus grant", () => {
    const existing = reconcileInterfaceComposerInitialFocusGrant(null, {
      sessionKey: "server-a:session-a",
      requested: false,
    });
    expect(existing).toBeNull();
    expect(
      resolveInterfaceComposerInitialFocusEffect({
        grant: existing,
        handledGrant: null,
        sessionKey: "server-a:session-a",
        screenActive: true,
        appActive: true,
        connected: true,
      }),
    ).toBe("ignore");

    const revisited = reconcileInterfaceComposerInitialFocusGrant(existing, {
      sessionKey: "server-a:session-a",
      requested: false,
    });
    expect(revisited).toBeNull();
  });

  test("an explicit new-Session grant focuses exactly once", () => {
    let grant = reconcileInterfaceComposerInitialFocusGrant(null, {
      sessionKey: "server-a:new-session",
      requested: true,
    });
    expect(
      resolveInterfaceComposerInitialFocusEffect({
        grant,
        handledGrant: null,
        sessionKey: "server-a:new-session",
        screenActive: true,
        appActive: true,
        connected: true,
      }),
    ).toBe("deliver");

    grant = consumeInterfaceComposerInitialFocusGrant(
      grant,
      "server-a:new-session",
    );
    expect(grant).toBeNull();

    for (const event of ["remount", "reconnect", "snapshot", "delta"]) {
      expect(
        resolveInterfaceComposerInitialFocusEffect({
          grant,
          handledGrant: null,
          sessionKey: "server-a:new-session",
          screenActive: true,
          appActive: true,
          connected: true,
        }),
        event,
      ).toBe("ignore");
    }
  });

  test("a grant waits for screen and connection readiness, then delivers once", () => {
    const grant = "server-a:new-session";
    let handledGrant: string | null = null;
    const effect = (screenActive: boolean, connected: boolean) =>
      resolveInterfaceComposerInitialFocusEffect({
        grant,
        handledGrant,
        sessionKey: grant,
        screenActive,
        appActive: true,
        connected,
      });

    expect(effect(false, false)).toBe("wait");
    expect(effect(true, false)).toBe("wait");
    expect(effect(true, true)).toBe("deliver");
    handledGrant = grant;
    expect(effect(true, true)).toBe("ignore");
  });

  test("one mounted surface handles a new B grant after consuming A", () => {
    let handledGrant: string | null = "server-a:session-a";
    const grant = "server-a:session-b";
    expect(
      resolveInterfaceComposerInitialFocusEffect({
        grant,
        handledGrant,
        sessionKey: grant,
        screenActive: true,
        appActive: true,
        connected: true,
      }),
    ).toBe("deliver");
    handledGrant = grant;
    expect(
      resolveInterfaceComposerInitialFocusEffect({
        grant,
        handledGrant,
        sessionKey: grant,
        screenActive: true,
        appActive: true,
        connected: true,
      }),
    ).toBe("ignore");
  });

  test("screen inactive waits while App inactive drops", () => {
    const input = {
      grant: "server-a:new-session",
      handledGrant: null,
      sessionKey: "server-a:new-session",
      connected: true,
    };
    expect(
      resolveInterfaceComposerInitialFocusEffect({
        ...input,
        screenActive: false,
        appActive: true,
      }),
    ).toBe("wait");
    expect(
      resolveInterfaceComposerInitialFocusEffect({
        ...input,
        screenActive: true,
        appActive: false,
      }),
    ).toBe("drop");
  });

  test("inactive lifecycle consumes an undelivered grant and active cannot replay it", () => {
    let grant = reconcileInterfaceComposerInitialFocusGrant(null, {
      sessionKey: "server-a:new-session",
      requested: true,
    });
    expect(
      resolveInterfaceComposerInitialFocusEffect({
        grant,
        handledGrant: null,
        sessionKey: "server-a:new-session",
        screenActive: true,
        appActive: false,
        connected: true,
      }),
    ).toBe("drop");

    grant = consumeInterfaceComposerInitialFocusGrant(
      grant,
      "server-a:new-session",
    );
    expect(
      resolveInterfaceComposerInitialFocusEffect({
        grant,
        handledGrant: null,
        sessionKey: "server-a:new-session",
        screenActive: true,
        appActive: true,
        connected: true,
      }),
    ).toBe("ignore");
  });

  test("a stale Session cannot consume or receive another Session's grant", () => {
    const grant = reconcileInterfaceComposerInitialFocusGrant(null, {
      sessionKey: "server-a:new-session",
      requested: true,
    });
    expect(
      consumeInterfaceComposerInitialFocusGrant(grant, "server-a:old-session"),
    ).toBe(grant);
    expect(
      resolveInterfaceComposerInitialFocusEffect({
        grant,
        handledGrant: null,
        sessionKey: "server-a:old-session",
        screenActive: true,
        appActive: true,
        connected: true,
      }),
    ).toBe("ignore");
  });

  test("only successful create navigation carries the one-shot route fact", () => {
    const sessions = source("../../app/(primary)/list.tsx");
    const existingOpen = sessions.slice(
      sessions.indexOf("const openAgent"),
      sessions.indexOf("const openContextMenu"),
    );
    const createdOpen = sessions.slice(
      sessions.indexOf("const finishCreateTerminal"),
      sessions.indexOf("const handleCreateTerminal"),
    );
    const serviceOpen = sessions.slice(
      sessions.indexOf("const openServiceTerminal"),
      sessions.indexOf("const openServiceURL"),
    );
    const notificationOpen = source("../../app/_layout.tsx");
    const brainOpen = source("../../app/(primary)/index.tsx");
    const inSessionCreate = source("./screen/useTerminalSessionActions.ts");

    expect(existingOpen).not.toContain("initialComposerFocus");
    expect(serviceOpen).not.toContain("initialComposerFocus");
    expect(notificationOpen).not.toContain("initialComposerFocus");
    expect(brainOpen).not.toContain("initialComposerFocus");
    expect(createdOpen).toContain('initialComposerFocus: "1"');
    expect(inSessionCreate).toContain('initialComposerFocus: "1"');
  });

  test("the route grant accepts one exact token and is made inert at the route owner", () => {
    const localState = source("./screen/useTerminalScreenLocalState.ts");
    const surfaceState = source("useInterfaceChatSurfaceState.ts");
    const keyboardFrame = source("InterfaceChatKeyboardFrame.tsx");

    expect(isInterfaceComposerInitialFocusRouteGrant("1")).toBe(true);
    for (const value of ["", "true", "once", "0", " 1 "]) {
      expect(isInterfaceComposerInitialFocusRouteGrant(value), value).toBe(
        false,
      );
    }
    expect(localState).toContain('initialComposerFocus: ""');
    expect(surfaceState).toContain(
      "handledInitialComposerFocusGrantRef.current = initialComposerFocusGrant",
    );
    expect(
      surfaceState.indexOf("onConsumeInitialComposerFocus?.()"),
    ).toBeLessThan(surfaceState.indexOf("composerInput.focus()"));
    expect(surfaceState).toContain('reason === "app"');
    // Lifecycle invalidation is reported through the UI-runtime dispatch result
    // so the app/route reasons reach the surface owner exactly as before.
    expect(keyboardFrame).toContain("invalidateReason");
    expect(keyboardFrame).toContain(
      "onKeyboardLifecycleInvalidate?.(result.invalidateReason)",
    );
  });

  test("manual native focus stays direct without a refocus timer path", () => {
    const input = source("InterfaceComposerInput.tsx");
    const hooks = source("InterfaceChatSurfaceHooks.ts");

    expect(input).toContain("showSoftInputOnFocus");
    expect(input).toContain("onFocus={onInputFocus}");
    expect(input).not.toContain("autoFocus");
    expect(hooks).toContain("inputRef.current?.focus()");
    expect(hooks).not.toContain("COMPOSER_REFOCUS_DELAYS_MS");
    expect(hooks).not.toContain("focusLockUntilRef");
  });
});
