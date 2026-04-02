import { buildAuthorizationHeader } from './auth';

export type ConnectionIssueCode =
  | 'invalid_url'
  | 'network_unreachable'
  | 'auth_required'
  | 'auth_invalid'
  | 'path_not_found'
  | 'proxy_error'
  | 'websocket_upgrade_failed'
  | 'unexpected_http_response';

export interface ConnectionIssue {
  code: ConnectionIssueCode;
  title: string;
  detail: string;
  hint: string;
  checkedAt: number;
  httpStatus?: number;
}

const PROBE_TIMEOUT_MS = 4000;

export async function diagnoseConnectionIssue(input: {
  serverUrl: string;
  secret?: string | null;
}): Promise<ConnectionIssue> {
  const probeURL = buildProbeURL(input.serverUrl);
  if (!probeURL) {
    return createIssue(
      'invalid_url',
      'Invalid endpoint URL',
      'The saved server URL could not be parsed.',
      'Edit the server and use a full ws:// or wss:// endpoint ending in /ws.',
    );
  }

  const authHeader = buildAuthorizationHeader(input.secret);
  const headers = authHeader ? { Authorization: authHeader } : undefined;

  try {
    const response = await fetchWithTimeout(probeURL, {
      method: 'GET',
      headers,
    });

    switch (response.status) {
      case 401:
        return createIssue(
          authHeader ? 'auth_invalid' : 'auth_required',
          authHeader ? 'Pairing secret rejected' : 'Pairing secret required',
          authHeader
            ? 'The daemon is reachable, but it rejected the configured secret.'
            : 'The daemon is reachable, but it requires a pairing secret before it will accept connections.',
          authHeader
            ? 'Update the secret in Settings or restart zen-daemon with the expected secret.'
            : 'Paste the 64-character secret from zen-daemon into Settings, or restart the daemon without -secret.',
          response.status,
        );
      case 404:
        return createIssue(
          'path_not_found',
          'Wrong WebSocket path',
          'The endpoint answered 404 for the WebSocket path.',
          'Use a URL ending in /ws, or update your reverse proxy or tunnel to forward /ws to zen-daemon.',
          response.status,
        );
      case 400:
      case 405:
      case 426:
        return createIssue(
          'websocket_upgrade_failed',
          'WebSocket handshake failed',
          'The daemon is reachable at this path, but the WebSocket upgrade did not succeed.',
          'Check HTTPS/TLS and make sure your reverse proxy forwards WebSocket Upgrade and Connection headers.',
          response.status,
        );
      case 502:
      case 503:
      case 504:
        return createIssue(
          'proxy_error',
          'Proxy could not reach daemon',
          `The endpoint returned HTTP ${response.status}, which usually means the proxy or tunnel could not reach zen-daemon.`,
          'Check that zen-daemon is running and that your reverse proxy or tunnel points at the correct local port.',
          response.status,
        );
      default:
        return createIssue(
          'unexpected_http_response',
          'Unexpected server response',
          `The endpoint returned HTTP ${response.status} instead of a WebSocket handshake response.`,
          'Make sure this URL points at zen-daemon rather than a normal website or HTTP-only endpoint.',
          response.status,
        );
    }
  } catch (error) {
    return buildNetworkIssue(error);
  }
}

export function connectionIssueAccent(issue: ConnectionIssue | null | undefined): string {
  if (!issue) return '#65758A';

  switch (issue.code) {
    case 'auth_required':
    case 'auth_invalid':
    case 'path_not_found':
    case 'websocket_upgrade_failed':
      return '#E7B65C';
    case 'network_unreachable':
    case 'proxy_error':
    case 'unexpected_http_response':
    case 'invalid_url':
      return '#F09999';
  }
}

function buildProbeURL(serverUrl: string): string | null {
  const trimmed = serverUrl.trim();
  if (!trimmed) return null;

  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol === 'ws:') {
      parsed.protocol = 'http:';
    } else if (parsed.protocol === 'wss:') {
      parsed.protocol = 'https:';
    } else if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
      return null;
    }

    parsed.search = '';
    parsed.hash = '';
    return parsed.toString();
  } catch {
    return null;
  }
}

async function fetchWithTimeout(
  url: string,
  init: RequestInit,
  timeoutMs: number = PROBE_TIMEOUT_MS,
): Promise<Response> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);

  try {
    return await fetch(url, {
      ...init,
      signal: controller.signal,
    });
  } finally {
    clearTimeout(timeout);
  }
}

function buildNetworkIssue(error: unknown): ConnectionIssue {
  const rawMessage = error instanceof Error ? error.message.trim() : '';
  const normalizedMessage = rawMessage.toLowerCase();

  if (error instanceof Error && error.name === 'AbortError') {
    return createIssue(
      'network_unreachable',
      'Server timed out',
      'The daemon did not respond before the probe timed out.',
      'Check the host or IP, tunnel latency, firewall rules, and whether zen-daemon is busy or sleeping.',
    );
  }

  if (
    normalizedMessage.includes('enotfound') ||
    normalizedMessage.includes('name resolution') ||
    normalizedMessage.includes('dns') ||
    normalizedMessage.includes('host not found')
  ) {
    return createIssue(
      'network_unreachable',
      'DNS lookup failed',
      rawMessage || 'The hostname could not be resolved before the WebSocket connection started.',
      'Check the hostname spelling, local DNS, or try a direct IP address instead of a hostname.',
    );
  }

  if (normalizedMessage.includes('econnrefused') || normalizedMessage.includes('connection refused')) {
    return createIssue(
      'network_unreachable',
      'Daemon refused connection',
      'The host is reachable, but nothing accepted the connection on the configured port.',
      'Check that zen-daemon is running and listening on the expected port, and that your tunnel or proxy forwards to the same port.',
    );
  }

  if (
    normalizedMessage.includes('certificate') ||
    normalizedMessage.includes('ssl') ||
    normalizedMessage.includes('tls')
  ) {
    return createIssue(
      'network_unreachable',
      'TLS handshake failed',
      rawMessage || 'The HTTPS/TLS handshake failed before the WebSocket could connect.',
      'Check your certificate chain, hostname, and reverse proxy TLS configuration.',
    );
  }

  return createIssue(
    'network_unreachable',
    'Server unreachable',
    rawMessage
      ? `The network request failed before a WebSocket connection could be established: ${rawMessage}`
      : 'The network request failed before a WebSocket connection could be established.',
    'Check the host or IP, DNS, tunnel, firewall, and whether zen-daemon is running.',
  );
}

function createIssue(
  code: ConnectionIssueCode,
  title: string,
  detail: string,
  hint: string,
  httpStatus?: number,
): ConnectionIssue {
  return {
    code,
    title,
    detail,
    hint,
    checkedAt: Date.now(),
    httpStatus,
  };
}
