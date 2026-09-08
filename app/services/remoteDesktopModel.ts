export function desktopURL(resolved: string, pinnedLink: boolean): string {
  const url = new URL(resolved);
  if (url.protocol !== "wss:" && !(pinnedLink && url.protocol === "ws:" && url.hostname === "127.0.0.1")) {
    throw new Error("Remote Desktop requires a secure server connection.");
  }
  if (url.username || url.password) throw new Error("Invalid desktop origin.");
  url.pathname = "/desktop";
  url.search = "";
  url.hash = "";
  return url.toString();
}

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
  | { type: "button" | "key"; code: number; down: boolean }
  | { type: "scroll"; delta: number }
  | { type: "release" };

export function desktopKey(code: number): DesktopInput[] {
  return [{ type: "key", code, down: true }, { type: "key", code, down: false }];
}

export function desktopText(text: string): DesktopInput[] {
  const events: DesktopInput[] = [];
  for (const char of text.slice(0, 16)) {
    const code = char.codePointAt(0)!;
    if (code < 32 || code > 126) continue;
    const shifted = /[A-Z~!@#$%^&*()_+{}|:"<>?]/.test(char);
    if (shifted) events.push({ type: "key", code: 0xffe1, down: true });
    events.push(...desktopKey(code));
    if (shifted) events.push({ type: "key", code: 0xffe1, down: false });
  }
  return events;
}

export function desktopTextEdits(previous: string, next: string): DesktopInput[][] {
  const before = previous.replace(/[^\x20-\x7e]/g, "");
  const after = next.replace(/[^\x20-\x7e]/g, "");
  let prefix = 0;
  while (prefix < before.length && before[prefix] === after[prefix]) prefix++;
  const batches: DesktopInput[][] = [];
  for (let remaining = before.length - prefix; remaining > 0; remaining -= 32) {
    batches.push(Array.from({ length: Math.min(remaining, 32) }, () => desktopKey(0xff08)).flat());
  }
  for (let offset = prefix; offset < after.length; offset += 16) {
    batches.push(desktopText(after.slice(offset, offset + 16)));
  }
  return batches;
}
