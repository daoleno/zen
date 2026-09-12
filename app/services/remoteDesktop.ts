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
  const authorization = await buildAuthorizationHeader({ daemonId: server.daemonId, purpose: "zen-desktop" });
  // The control channel may go through a local pinned tunnel while the native
  // engine needs its own directly reachable computer IP. Prefer Moonlight when
  // the engine endpoint is direct, before selecting the pinned-link control plan.
  const lanOrigin = desktopLanOrigin(server);
  const engineHost = lanOrigin ? new URL(lanOrigin).hostname : new URL(server.url).hostname;
  const engineDirect = server.transportKind !== "link" &&
    !/^(127\.|localhost$|\[?::1)/i.test(engineHost);
  if (capability.moonlight?.available && capability.scopeVersion > 0 && engineDirect) {
    if (signal?.aborted) throw new Error("Desktop connection cancelled.");
    return JSON.stringify({
      transport: "moonlight",
      authorization,
      inputGeneration,
      moonlight: { ...capability.moonlight, host: engineHost },
    });
  }
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
  return JSON.stringify({ ...plan, authorization, inputGeneration, mode: "unattended" });
}
