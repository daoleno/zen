export function normalizeServerSecret(rawValue: string | null | undefined): string {
  const trimmed = rawValue?.trim() || '';
  if (!trimmed) return '';
  return /^[0-9a-f]{64}$/i.test(trimmed) ? trimmed.toLowerCase() : '';
}

export function buildAuthorizationHeader(secret: string | null | undefined): string | null {
  const normalized = normalizeServerSecret(secret);
  if (!normalized) return null;
  return `Bearer ${normalized}`;
}
