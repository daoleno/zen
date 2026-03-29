export function makeSessionKey(serverId: string, agentId: string): string {
  return JSON.stringify([serverId, agentId]);
}

export function parseSessionKey(sessionKey: string): { serverId: string; agentId: string } | null {
  try {
    const parsed = JSON.parse(sessionKey) as unknown;
    if (!Array.isArray(parsed) || parsed.length !== 2) {
      return null;
    }

    const [serverId, agentId] = parsed;
    if (typeof serverId !== 'string' || typeof agentId !== 'string') {
      return null;
    }

    return { serverId, agentId };
  } catch {
    return null;
  }
}
