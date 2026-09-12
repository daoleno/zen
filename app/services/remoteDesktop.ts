import { buildAuthorizationHeader, verifyDaemonAssertion } from "./auth";
import { resolveStoredServerURL } from "./pinnedTransport";
import type { StoredServer } from "./storage";
import { desktopLanOrigin, desktopPinnedIdentityPlan, desktopTransportPlan } from "./desktopTransportPolicy";
import { fetch } from "expo/fetch";
import {
  desktopPreflightError,
  fetchDesktopCapability,
  type DesktopProofDependencies,
  verifyDesktopServer,
} from "./desktopConnectionCheck";

export async function prepareDesktopConnection(server: StoredServer, inputGeneration: string, signal?: AbortSignal): Promise<string> {
  const resolved = await resolveStoredServerURL(server);
  const proof: DesktopProofDependencies = {
    fetch,
    authorization: (purpose) => buildAuthorizationHeader({ daemonId: server.daemonId, purpose }),
    verify: verifyDaemonAssertion,
  };
  await verifyDesktopServer(server, resolved, proof, signal);
  const capability = await fetchDesktopCapability(server, resolved, proof, signal);
  const blocking = desktopPreflightError(capability);
  const identityLan = capability.identityTls && !!desktopLanOrigin(server) && server.transportKind !== "link"
    && capability.scopeVersion === 1 && capability.unattended;
  if (blocking && !identityLan) throw blocking;
  if (signal?.aborted) throw new Error("Desktop connection cancelled.");
  let plan;
  if (identityLan) {
    const source = new URL(server.url);
    const port = Number(source.port) || 9876;
    const { startPinnedTunnel } = await import("../modules/zen-link-transport/src");
    const tunnel = await startPinnedTunnel(`desktop:${server.id}`, source.hostname, port, capability.transportPin, "on-demand");
    plan = desktopPinnedIdentityPlan(`ws://127.0.0.1:${tunnel.port}`, `wss://${source.hostname}:${port}`, capability.transportPin);
  } else {
    plan = desktopTransportPlan(server, resolved);
    if (plan.transport === "trusted-lan") {
      throw new Error("Unattended desktop cannot use unencrypted LAN transport.");
    }
  }
  const authorization = await buildAuthorizationHeader({ daemonId: server.daemonId, purpose: "zen-desktop" });
  if (capability.moonlight?.available && capability.scopeVersion > 0) {
    if (signal?.aborted) throw new Error("Desktop connection cancelled.");
    // The daemon advertised an explicitly configured Sunshine host under the
    // signed desktop scope; the native view consumes this bootstrap directly.
    return JSON.stringify({
      transport: "moonlight",
      authorization,
      inputGeneration,
      moonlight: capability.moonlight,
    });
  }
  return JSON.stringify({ ...plan, authorization, inputGeneration, mode: "unattended" });
}
