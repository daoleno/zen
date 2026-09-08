import { buildAuthorizationHeader } from "./auth";
import { resolveStoredServerURL } from "./pinnedTransport";
import type { StoredServer } from "./storage";
import { desktopURL } from "./remoteDesktopModel";

export async function prepareDesktopConnection(server: StoredServer): Promise<string> {
  const resolved = await resolveStoredServerURL(server);
  const url = desktopURL(resolved, server.transportKind === "link");
  const authorization = await buildAuthorizationHeader({ daemonId: server.daemonId, purpose: "zen-desktop" });
  return JSON.stringify({ url, authorization });
}
