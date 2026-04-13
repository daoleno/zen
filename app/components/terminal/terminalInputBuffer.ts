const BACKSPACE = '\x7f';

function splitInputUnits(text: string): string[] {
  return Array.from(text);
}

export function trimTrailingInputUnits(text: string, count: number): string {
  if (count <= 0 || !text) {
    return text;
  }

  const units = splitInputUnits(text);
  if (units.length === 0) {
    return '';
  }

  return units.slice(0, Math.max(0, units.length - count)).join('');
}

export interface TerminalInputDelta {
  backspaces: number;
  insertedText: string;
}

export function diffTerminalInput(previous: string, next: string): TerminalInputDelta {
  if (previous === next) {
    return {
      backspaces: 0,
      insertedText: '',
    };
  }

  const previousUnits = splitInputUnits(previous);
  const nextUnits = splitInputUnits(next);
  const maxSharedPrefix = Math.min(previousUnits.length, nextUnits.length);

  let sharedPrefixLength = 0;
  while (
    sharedPrefixLength < maxSharedPrefix &&
    previousUnits[sharedPrefixLength] === nextUnits[sharedPrefixLength]
  ) {
    sharedPrefixLength += 1;
  }

  return {
    backspaces: previousUnits.length - sharedPrefixLength,
    insertedText: nextUnits.slice(sharedPrefixLength).join(''),
  };
}

export function encodeTerminalInputDelta(delta: TerminalInputDelta): string {
  if (delta.backspaces <= 0 && !delta.insertedText) {
    return '';
  }

  return `${BACKSPACE.repeat(delta.backspaces)}${delta.insertedText}`;
}
