export function applyCtrlModifier(input: string): string {
  if (input.length !== 1) {
    return input;
  }

  if (input === ' ') {
    return '\x00';
  }

  if (input === '?') {
    return '\x7f';
  }

  const code = input.charCodeAt(0);
  const upperCode = code >= 97 && code <= 122 ? code - 32 : code;
  if (upperCode >= 64 && upperCode <= 95) {
    return String.fromCharCode(upperCode - 64);
  }

  return input;
}
