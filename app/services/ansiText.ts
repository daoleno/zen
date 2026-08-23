export function stripAnsiText(raw: string): string {
  return raw
    .replace(/\x1b\[[0-9;]*m/g, '')
    .replace(/\s+/g, ' ')
    .trim();
}
