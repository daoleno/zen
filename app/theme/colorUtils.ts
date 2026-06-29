export function parseHex(value: string): { red: number; green: number; blue: number } {
  const normalized = value.trim().replace('#', '');
  return {
    red: parseInt(normalized.slice(0, 2), 16),
    green: parseInt(normalized.slice(2, 4), 16),
    blue: parseInt(normalized.slice(4, 6), 16),
  };
}

export function rgbToHex(red: number, green: number, blue: number): string {
  return (
    '#' +
    red.toString(16).padStart(2, '0') +
    green.toString(16).padStart(2, '0') +
    blue.toString(16).padStart(2, '0')
  );
}

export function mixHex(from: string, to: string, weight: number): string {
  const start = parseHex(from);
  const end = parseHex(to);
  const factor = clamp(weight, 0, 1);
  return rgbToHex(
    Math.round(start.red + (end.red - start.red) * factor),
    Math.round(start.green + (end.green - start.green) * factor),
    Math.round(start.blue + (end.blue - start.blue) * factor),
  );
}

export function relativeLuminance(hex: string): number {
  const { red, green, blue } = parseHex(hex);
  const channel = (value: number) => {
    const scaled = value / 255;
    return scaled <= 0.03928
      ? scaled / 12.92
      : ((scaled + 0.055) / 1.055) ** 2.4;
  };
  return (
    0.2126 * channel(red) +
    0.7152 * channel(green) +
    0.0722 * channel(blue)
  );
}

export function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}