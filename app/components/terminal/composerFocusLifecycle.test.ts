import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  COMPOSER_STALE_HIDE_GRACE_MS,
  INITIAL_COMPOSER_FOCUS_LIFECYCLE_STATE,
  reduceComposerFocusLifecycle,
  resolvePendingComposerFocusHide,
} from "./composerFocusLifecycle";

const T0 = 1_000_000;
const focus = () => reduceComposerFocusLifecycle(INITIAL_COMPOSER_FOCUS_LIFECYCLE_STATE, { type: "input_focus" });
const hide = (state: ReturnType<typeof focus>, at = T0) =>
  reduceComposerFocusLifecycle(state, { type: "keyboard_hide", at });
const show = (state: ReturnType<typeof focus>) =>
  reduceComposerFocusLifecycle(state, { type: "keyboard_show" });
const settle = (
  state: ReturnType<typeof focus>,
  input: { now?: number; keyboardVisible?: boolean } = {},
) =>
  resolvePendingComposerFocusHide(state, {
    now: input.now ?? T0 + COMPOSER_STALE_HIDE_GRACE_MS,
    keyboardVisible: input.keyboardVisible ?? false,
  });

describe("Composer focus lifecycle", () => {
  test("stale hide from the previous IME epoch cannot collapse a new focus", () => {
    const focused = focus();
    const deferred = hide(focused);
    expect(deferred.inputFocused).toBe(true);
    // The matching show for the current epoch cancels the deferral entirely.
    expect(show(deferred).inputFocused).toBe(true);
    expect(show(deferred).pendingHide).toBeNull();
  });

  test("a hide without a focus epoch is inert", () => {
    expect(hide(INITIAL_COMPOSER_FOCUS_LIFECYCLE_STATE)).toBe(
      INITIAL_COMPOSER_FOCUS_LIFECYCLE_STATE,
    );
  });

  test("blur always closes the current epoch and cancels the deferral", () => {
    const deferred = hide(focus());
    const blurred = reduceComposerFocusLifecycle(deferred, { type: "input_blur" });
    expect(blurred.inputFocused).toBe(false);
    expect(blurred.pendingHide).toBeNull();
  });

  test("sequence: stale hide then new show keeps the expansion", () => {
    // Old epoch hide lands after the new focus; the new show arrives before
    // the settle recheck would run.
    const focused = focus();
    const deferred = hide(focused);
    const shown = show(deferred);
    expect(shown.inputFocused).toBe(true);
    expect(shown.pendingHide).toBeNull();
    expect(settle(shown).inputFocused).toBe(true);
  });

  test("sequence: focus while keyboard already visible collapses on a later hide", () => {
    // IME was open before focus (no new show event); a back-press hide is the
    // real dismissal and must collapse after the deferred visibility recheck.
    const preShown = show(INITIAL_COMPOSER_FOCUS_LIFECYCLE_STATE);
    const focused = reduceComposerFocusLifecycle(preShown, { type: "input_focus" });
    const deferred = hide(focused, T0 + 400);
    expect(deferred.inputFocused).toBe(true);
    const settled = settle(deferred, { now: T0 + 400 + COMPOSER_STALE_HIDE_GRACE_MS });
    expect(settled.inputFocused).toBe(false);
    expect(settled.pendingHide).toBeNull();
  });

  test("sequence: immediate real hide collapses through the deferred recheck", () => {
    const focused = show(focus());
    const deferred = hide(focused, T0);
    expect(deferred.inputFocused).toBe(true);
    expect(settle(deferred).inputFocused).toBe(false);
  });

  test("deferred callback cannot affect an updated focus epoch", () => {
    const first = hide(focus(), T0);
    expect(first.pendingHide?.epoch).toBe(1);
    // A newer focus epoch replaces the deferred one before the callback runs.
    const refocused = reduceComposerFocusLifecycle(first, { type: "input_focus" });
    expect(refocused.focusEpoch).toBe(2);
    expect(refocused.pendingHide).toBeNull();
    expect(settle(refocused, { now: T0 + 10_000 }).inputFocused).toBe(true);
  });

  test("recheck before the grace window elapses defers the decision", () => {
    const deferred = hide(show(focus()), T0);
    expect(settle(deferred, { now: T0 + 1 }).inputFocused).toBe(true);
    expect(settle(deferred, { now: T0 + 1 }).pendingHide).not.toBeNull();
  });

  test("recheck with the keyboard still visible cancels the deferral", () => {
    const deferred = hide(show(focus()), T0);
    const settled = settle(deferred, { keyboardVisible: true });
    expect(settled.inputFocused).toBe(true);
    expect(settled.pendingHide).toBeNull();
  });

  test("duplicate hides never extend the original deferral deadline", () => {
    const deferred = hide(show(focus()), T0);
    const firstDeadline = deferred.pendingHide?.at;
    // A second hide while the first is still pending is inert: the policy
    // keeps the original timestamp instead of restarting the grace window.
    const duplicated = hide(deferred, T0 + 60);
    expect(duplicated).toBe(deferred);
    expect(duplicated.pendingHide?.at).toBe(firstDeadline);
    // The original deadline still collapses the epoch.
    expect(
      settle(duplicated, { now: T0 + COMPOSER_STALE_HIDE_GRACE_MS })
        .inputFocused,
    ).toBe(false);
  });

  test("every input_focus starts a new epoch token", () => {
    expect(focus().focusEpoch).toBe(1);
    expect(
      reduceComposerFocusLifecycle(focus(), { type: "input_focus" }).focusEpoch,
    ).toBe(2);
  });
});

describe("Composer focus lifecycle hook wiring", () => {
  const hooks = readFileSync(
    join(import.meta.dir, "InterfaceChatSurfaceHooks.ts"),
    "utf8",
  );

  test("hide handling defers through the grace window and rechecks native visibility", () => {
    expect(hooks).toContain('type: "keyboard_hide",\n        at: Date.now(),');
    expect(hooks).toContain("scheduleHideRecheck()");
    expect(hooks).toContain("COMPOSER_STALE_HIDE_GRACE_MS");
    expect(hooks).toContain("Keyboard.isVisible()");
    expect(hooks).toContain("resolvePendingComposerFocusHide");
  });

  test("the recheck is scheduled only when the hide newly opens the deferral window", () => {
    expect(hooks).toContain(
      "if (next.pendingHide !== null && previous.pendingHide === null) {",
    );
  });

  test("show, focus, and blur cancel the deferred recheck", () => {
    expect(hooks).toContain("keyboardDidShow");
    expect(hooks).toContain("applyFocusLifecycle({ type: \"keyboard_show\" });");
    expect(hooks).toContain("cancelHideRecheck();");
    expect(hooks).toContain("handleFocus");
    expect(hooks).toContain("handleBlur");
  });

  test("collapse keeps the native input focus in sync", () => {
    expect(hooks).toContain(
      "if (previous.inputFocused && next.inputFocused === false) {\n        inputRef.current?.blur();\n      }",
    );
  });
});
