/**
 * Composer focus lifecycle policy.
 *
 * Android can deliver keyboardDidHide from the previous IME epoch after a new
 * TextInput onFocus. That stale hide must not collapse an already-focused
 * Composer. Instead of trusting the event ordering alone, a hide that arrives
 * while the input is focused opens a short grace window: the policy defers the
 * hide and, when the window settles, rechecks the native keyboard visibility.
 * The epoch token makes a late deferred callback unable to collapse a focus
 * epoch that has since been replaced.
 */

/** Short settle window between a hide event and the native visibility recheck. */
export const COMPOSER_STALE_HIDE_GRACE_MS = 120;

export type ComposerFocusLifecycleState = {
  inputFocused: boolean;
  /** Focus epoch token; every input_focus starts a new epoch. */
  focusEpoch: number;
  /**
   * Hide deferred for recheck. `epoch` is the focus epoch that received the
   * hide; a newer focus epoch invalidates the deferral entirely.
   */
  pendingHide: { at: number; epoch: number } | null;
};

export type ComposerFocusLifecycleEvent =
  | { type: "input_focus" }
  | { type: "input_blur" }
  | { type: "keyboard_show" }
  | { type: "keyboard_hide"; at: number };

export const INITIAL_COMPOSER_FOCUS_LIFECYCLE_STATE: ComposerFocusLifecycleState = {
  inputFocused: false,
  focusEpoch: 0,
  pendingHide: null,
};

export function reduceComposerFocusLifecycle(
  state: ComposerFocusLifecycleState,
  event: ComposerFocusLifecycleEvent,
): ComposerFocusLifecycleState {
  switch (event.type) {
    case "input_focus":
      // A new focus epoch cancels any deferred hide from the previous epoch.
      return {
        inputFocused: true,
        focusEpoch: state.focusEpoch + 1,
        pendingHide: null,
      };
    case "input_blur":
      return {
        inputFocused: false,
        focusEpoch: state.focusEpoch,
        pendingHide: null,
      };
    case "keyboard_show":
      // A show proves the keyboard is visible again: cancel any deferred hide.
      return { ...state, pendingHide: null };
    case "keyboard_hide":
      if (!state.inputFocused) return state;
      if (state.pendingHide !== null) return state;
      return {
        ...state,
        pendingHide: { at: event.at, epoch: state.focusEpoch },
      };
  }
}

/**
 * Settle a deferred hide once the grace window has elapsed. Collapses the
 * Composer only when the native keyboard is confirmed hidden and the focus
 * epoch that received the hide is still current.
 */
export function resolvePendingComposerFocusHide(
  state: ComposerFocusLifecycleState,
  input: {
    now: number;
    keyboardVisible: boolean;
    graceMs?: number;
  },
): ComposerFocusLifecycleState {
  const pending = state.pendingHide;
  if (!pending) return state;
  if (pending.epoch !== state.focusEpoch) {
    // A newer focus epoch owns the input: the deferred hide is obsolete.
    return { ...state, pendingHide: null };
  }
  const graceMs = input.graceMs ?? COMPOSER_STALE_HIDE_GRACE_MS;
  if (input.now - pending.at < graceMs) return state;
  if (input.keyboardVisible) {
    // The IME is (still) visible: the hide event was spurious or belongs to
    // another surface. Keep the expanded Composer.
    return { ...state, pendingHide: null };
  }
  return {
    inputFocused: false,
    focusEpoch: state.focusEpoch,
    pendingHide: null,
  };
}
