import { parseConnectLink } from './connection';
import { markOnboarded, saveServer, type StoredServer } from './storage';
import { wsClient } from './websocket';

export interface ImportConnectionOptions {
  onImported?: (server: StoredServer) => void | Promise<void>;
}

export async function importConnection(
  rawValue: string,
  options: ImportConnectionOptions = {},
): Promise<StoredServer | null> {
  const payload = parseConnectLink(rawValue);
  if (!payload) {
    return null;
  }

  const savedServer = await saveServer({
    name: payload.name || '',
    provider: payload.provider,
    endpoint: payload.endpoint,
    secret: payload.secret,
  });

  await markOnboarded();
  wsClient.connectServer(savedServer);
  await options.onImported?.(savedServer);
  return savedServer;
}
