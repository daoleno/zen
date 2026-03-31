import { normalizeServerSecret } from './auth';

export type ConnectionProvider = 'custom-endpoint' | 'local-lan';

export interface ConnectionImportPayload {
  provider: ConnectionProvider;
  endpoint: string;
  name?: string;
  secret?: string;
}

const DEFAULT_LAN_PORT = '9876';

export function normalizeConnectionProvider(
  value: string | null | undefined,
  fallbackValue: string | null | undefined = '',
): ConnectionProvider {
  if (value === 'local-lan' || value === 'custom-endpoint') {
    return value;
  }

  const fallback = fallbackValue?.trim() || '';
  if (fallback && !/^[a-z]+:\/\//i.test(fallback)) {
    return 'local-lan';
  }

  return 'custom-endpoint';
}

export function normalizeServerURL(rawValue: string): string {
  const trimmed = rawValue.trim();
  if (!trimmed) return '';

  const withProtocol = /^[a-z]+:\/\//i.test(trimmed) ? trimmed : `ws://${trimmed}`;

  try {
    const parsed = new URL(withProtocol);
    if (parsed.protocol === 'http:') parsed.protocol = 'ws:';
    if (parsed.protocol === 'https:') parsed.protocol = 'wss:';

    if (parsed.pathname === '' || parsed.pathname === '/') {
      parsed.pathname = '/ws';
    }

    parsed.hash = '';
    return parsed.toString();
  } catch {
    return trimmed;
  }
}

export function buildServerURL(provider: ConnectionProvider, endpoint: string): string {
  const trimmed = endpoint.trim();
  if (!trimmed) return '';

  if (provider === 'custom-endpoint') {
    return normalizeServerURL(trimmed);
  }

  if (/^[a-z]+:\/\//i.test(trimmed)) {
    return normalizeServerURL(trimmed);
  }

  const withPort = hasExplicitPort(trimmed) ? trimmed : `${trimmed}:${DEFAULT_LAN_PORT}`;
  return normalizeServerURL(withPort);
}

export function deriveEndpointFromURL(provider: ConnectionProvider, url: string): string {
  if (provider === 'custom-endpoint') {
    return url;
  }

  try {
    const parsed = new URL(url);
    return parsed.host || url;
  } catch {
    return url;
  }
}

export function parseConnectLink(rawValue: string): ConnectionImportPayload | null {
  const trimmed = rawValue.trim();
  if (!trimmed) return null;

  if (trimmed.startsWith('{')) {
    try {
      const parsed = JSON.parse(trimmed) as Record<string, unknown>;
      const endpoint = readString(parsed.endpoint) || readString(parsed.url);
      if (!endpoint) return null;
      return {
        provider: normalizeConnectionProvider(readString(parsed.provider), endpoint),
        endpoint,
        name: readString(parsed.name) || undefined,
        secret: normalizeServerSecret(readString(parsed.secret)) || undefined,
      };
    } catch {
      return null;
    }
  }

  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol !== 'zen:') {
      return null;
    }

    const endpoint = parsed.searchParams.get('endpoint') || parsed.searchParams.get('url') || '';
    if (!endpoint) return null;

    return {
      provider: normalizeConnectionProvider(parsed.searchParams.get('provider'), endpoint),
      endpoint,
      name: parsed.searchParams.get('name')?.trim() || undefined,
      secret: normalizeServerSecret(parsed.searchParams.get('secret')) || undefined,
    };
  } catch {
    return null;
  }
}

function hasExplicitPort(value: string): boolean {
  const candidate = value.trim();
  if (!candidate) return false;

  const bracketIndex = candidate.lastIndexOf(']');
  if (candidate.startsWith('[') && bracketIndex >= 0) {
    return candidate.slice(bracketIndex + 1).startsWith(':');
  }

  return candidate.split(':').length > 1;
}

function readString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}
