export function desktopStart(source: unknown, control: boolean) {
  if (source !== "x11" && source !== "wayland") return null;
  return { type: "start" as const, source, control };
}

export function desktopPoint(x: number, y: number, viewportWidth: number, viewportHeight: number, frameWidth: number, frameHeight: number) {
  if (![x, y, viewportWidth, viewportHeight, frameWidth, frameHeight].every(Number.isFinite) ||
    Math.min(viewportWidth, viewportHeight, frameWidth, frameHeight) <= 0) return null;
  const scale = Math.min(viewportWidth / frameWidth, viewportHeight / frameHeight);
  const width = frameWidth * scale;
  const height = frameHeight * scale;
  const px = (x - (viewportWidth - width) / 2) / width;
  const py = (y - (viewportHeight - height) / 2) / height;
  return px < 0 || px > 1 || py < 0 || py > 1 ? null : { x: px, y: py };
}

export type DesktopInput =
  | { type: "pointer"; x: number; y: number }
  | { type: "text"; code: number }
  | { type: "button" | "key"; code: number; down: boolean }
  | { type: "scroll"; delta: number }
  | { type: "release" };

export interface DesktopPanState {
  point: { x: number; y: number };
  offset: { x: number; y: number };
}

/**
 * Pan state survives PanResponder recreation: the screen rebuilds the responder
 * on every offset render, so a per-instance gestureState.dx only ever reflects
 * the last fragment. Callers keep this state object in a persistent ref and
 * hand it back on each move; coordinates are page units.
 */
export function beginDesktopPan(offset: { x: number; y: number }, point: { x: number; y: number }): DesktopPanState {
  return { point: { x: point.x, y: point.y }, offset: { x: offset.x, y: offset.y } };
}

export function advanceDesktopPan(state: DesktopPanState, point: { x: number; y: number }): DesktopPanState {
  return {
    point: { x: point.x, y: point.y },
    offset: { x: state.offset.x + point.x - state.point.x, y: state.offset.y + point.y - state.point.y },
  };
}

export function desktopKey(code: number): DesktopInput[] {
  return [{ type: "key", code, down: true }, { type: "key", code, down: false }];
}

export type DesktopNamedKey = "Backspace" | "Enter" | "Delete";

// X keysyms for named (non-text) editing keys; the phone emits these instead of
// guessing keycodes, so the host keymap decides the physical mapping.
const NAMED_KEYS: Record<DesktopNamedKey, number> = { Backspace: 0xff08, Enter: 0xff0d, Delete: 0xffff };

// The daemon rejects any batch above this bound and ends the connection, so a
// paste or IME commit must be split before it reaches the command queue.
export const MAX_DESKTOP_BATCH_EVENTS = 64;

export function desktopNamedKey(key: DesktopNamedKey): DesktopInput[] {
  return desktopKey(NAMED_KEYS[key]);
}

/**
 * Committed text from the phone IME becomes one atomic character event per
 * Unicode scalar. Composing text never reaches here: the native input reports
 * only commitText/shouldChangeCharactersIn results. Control characters are
 * dropped because Enter/Backspace travel as named keys.
 */
export function desktopText(text: string): DesktopInput[] {
  const events: DesktopInput[] = [];
  for (const char of text) {
    const code = char.codePointAt(0);
    if (code === undefined || code < 0x20 || code > 0x10ffff || code === 0x7f) continue;
    if (code >= 0xd800 && code <= 0xdfff) continue;
    events.push({ type: "text", code });
  }
  return events;
}

/**
 * Committed phone keyboard text becomes ordered batches of at most
 * MAX_DESKTOP_BATCH_EVENTS protocol events. Newlines become dedicated Enter
 * key events; other control characters are dropped because named keys carry
 * them. A key press and its release always stay in the same batch.
 */
export function desktopCommittedText(text: string): DesktopInput[][] {
  const batches: DesktopInput[][] = [];
  let batch: DesktopInput[] = [];
  const push = (events: DesktopInput[]) => {
    if (batch.length + events.length > MAX_DESKTOP_BATCH_EVENTS) {
      if (batch.length) batches.push(batch);
      batch = [];
    }
    batch.push(...events);
  };
  // Iterate whole Unicode scalars, not UTF-16 code units, so an astral-plane
  // character stays one event and never leaves a lone surrogate behind.
  const characters = Array.from(text);
  for (let index = 0; index < characters.length; index++) {
    const char = characters[index];
    if (char === "\n" || char === "\r") {
      if (char === "\r" && characters[index + 1] === "\n") index++;
      push(desktopNamedKey("Enter"));
      continue;
    }
    const code = char.codePointAt(0);
    if (code === undefined || code < 0x20 || code > 0x10ffff || code === 0x7f) continue;
    if (code >= 0xd800 && code <= 0xdfff) continue;
    push([{ type: "text", code }]);
  }
  if (batch.length) batches.push(batch);
  return batches;
}

/**
 * Fallback path for a native shell without the commit-aware keyboard view: the
 * React Native TextInput mirrors local text, so the diff is replayed as
 * deletions plus committed characters.
 */
export function desktopTextEdits(previous: string, next: string): DesktopInput[][] {
  const before = Array.from(previous);
  const after = Array.from(next);
  let prefix = 0;
  while (prefix < before.length && prefix < after.length && before[prefix] === after[prefix]) prefix++;
  const batches: DesktopInput[][] = [];
  for (let remaining = before.length - prefix; remaining > 0; remaining -= 32) {
    batches.push(Array.from({ length: Math.min(remaining, 32) }, () => desktopNamedKey("Backspace")).flat());
  }
  for (let offset = prefix; offset < after.length; offset += 16) {
    batches.push(desktopText(after.slice(offset, offset + 16).join("")));
  }
  return batches.filter((events) => events.length > 0);
}
