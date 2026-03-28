import React from 'react';
import { Text } from 'react-native';
import { Colors, Typography } from '../constants/tokens';

interface AnsiSegment {
  text: string;
  bold: boolean;
  color: string;
}

const ANSI_COLORS: Record<number, string> = {
  30: '#555555', // black
  31: '#FF5252', // red
  32: '#4CAF50', // green
  33: '#FFB74D', // yellow
  34: '#5B9DFF', // blue
  35: '#B388FF', // magenta
  36: '#4FC3F7', // cyan
  37: '#E8E8ED', // white
  90: '#888888', // bright black (grey)
  91: '#FF8A80', // bright red
  92: '#69F0AE', // bright green
  93: '#FFD54F', // bright yellow
  94: '#82B1FF', // bright blue
  95: '#EA80FC', // bright magenta
  96: '#80DEEA', // bright cyan
  97: '#FFFFFF', // bright white
};

export function parseAnsiLine(raw: string): AnsiSegment[] {
  const segments: AnsiSegment[] = [];
  let bold = false;
  let color: string = Colors.textPrimary;
  let buffer = '';

  const regex = /\x1b\[([0-9;]*)m/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = regex.exec(raw)) !== null) {
    // Add text before this escape.
    if (match.index > lastIndex) {
      buffer = raw.slice(lastIndex, match.index);
      if (buffer) segments.push({ text: buffer, bold, color });
    }

    // Parse codes.
    const codes = match[1].split(';').map(Number);
    for (const code of codes) {
      if (code === 0) { bold = false; color = Colors.textPrimary; }
      else if (code === 1) { bold = true; }
      else if (ANSI_COLORS[code]) { color = ANSI_COLORS[code]; }
    }

    lastIndex = regex.lastIndex;
  }

  // Remaining text.
  if (lastIndex < raw.length) {
    segments.push({ text: raw.slice(lastIndex), bold, color });
  }

  if (segments.length === 0 && raw.length > 0) {
    segments.push({ text: raw, bold: false, color: Colors.textPrimary });
  }

  return segments;
}

export function AnsiLine({ text }: { text: string }) {
  const segments = parseAnsiLine(text);

  if (segments.length === 1 && !segments[0].bold && segments[0].color === Colors.textPrimary) {
    return (
      <Text style={{
        color: Colors.textPrimary,
        fontFamily: Typography.terminalFont,
        fontSize: Typography.terminalSize,
        lineHeight: Typography.terminalSize * 1.6,
      }}>
        {segments[0].text}
      </Text>
    );
  }

  return (
    <Text style={{
      fontFamily: Typography.terminalFont,
      fontSize: Typography.terminalSize,
      lineHeight: Typography.terminalSize * 1.6,
    }}>
      {segments.map((seg, i) => (
        <Text key={i} style={{
          color: seg.color,
          fontWeight: seg.bold ? '700' : '400',
        }}>
          {seg.text}
        </Text>
      ))}
    </Text>
  );
}
