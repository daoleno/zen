import { buildAuthorizationHeader, verifyDaemonAssertion } from "./auth";
import { resolveStoredServerURL } from "./pinnedTransport";
import type { StoredServer } from "./storage";
import { desktopTransportPlan } from "./desktopTransportPolicy";
import { fetch } from "expo/fetch";
import { verifyDesktopServer } from "./desktopConnectionCheck";

export async function prepareDesktopConnection(server: StoredServer, inputGeneration: string, signal?: AbortSignal): Promise<string> {
  const resolved = await resolveStoredServerURL(server);
  const plan = desktopTransportPlan(server, resolved);
  await verifyDesktopServer(server, plan.url, { fetch, authorization: () => buildAuthorizationHeader({ daemonId: server.daemonId, purpose: "zen-probe" }), verify: verifyDaemonAssertion }, signal);
  if (signal?.aborted) throw new Error("Desktop connection cancelled.");
  const authorization = await buildAuthorizationHeader({ daemonId: server.daemonId, purpose: "zen-desktop" });
  return JSON.stringify({ ...plan, authorization, inputGeneration });
}
