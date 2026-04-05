import { parseConnectLink } from "./connection";
import { markOnboarded, saveServer, type StoredServer } from "./storage";
import { wsClient } from "./websocket";
import { enrollWithDaemon } from "./pairing";

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

  if (!payload.daemonPublicKey || !payload.enrollmentToken) {
    return null;
  }

  const pairing = await enrollWithDaemon({
    serverUrl: payload.url,
    daemonId: payload.daemonId,
    daemonPublicKey: payload.daemonPublicKey,
    enrollmentToken: payload.enrollmentToken,
  });

  const savedServer = await saveServer({
    name: payload.name || "",
    url: payload.url,
    daemonId: pairing.daemonId,
    daemonPublicKey: pairing.daemonPublicKey,
  });

  await markOnboarded();
  wsClient.connectServer(savedServer);
  await options.onImported?.(savedServer);
  return savedServer;
}
