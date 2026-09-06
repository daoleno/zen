export function makeSessionKey(serverId: string, workerId: string): string {
  return JSON.stringify([serverId, workerId]);
}

export function parseSessionKey(sessionKey: string): { serverId: string; workerId: string } | null {
  try {
    const parsed = JSON.parse(sessionKey) as unknown;
    if (!Array.isArray(parsed) || parsed.length !== 2) {
      return null;
    }

    const [serverId, workerId] = parsed;
    if (typeof serverId !== 'string' || typeof workerId !== 'string') {
      return null;
    }

    return { serverId, workerId };
  } catch {
    return null;
  }
}
