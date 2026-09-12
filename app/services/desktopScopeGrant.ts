import { buildAuthorizationHeader, getOrCreateLocalDeviceIdentity, verifyDaemonAssertion } from "./auth";
import { resolveStoredServerURL } from "./pinnedTransport";
import type { StoredServer } from "./storage";
import { fetch as expoFetch } from "expo/fetch";
import { fetchDesktopCapability, verifyDesktopServer, type DesktopProofDependencies } from "./desktopConnectionCheck";
import { DESKTOP_GRANT_PURPOSE, desktopGrantTransport, postDesktopGrant } from "./desktopScopeGrantCore";

export { DESKTOP_GRANT_PURPOSE, DESKTOP_GRANT_VERSION, desktopGrantTransport, postDesktopGrant } from "./desktopScopeGrantCore";
export type { DesktopGrantRequestDependencies, DesktopGrantTunnelFactory } from "./desktopScopeGrantCore";

// enableDesktopScope is the explicit in-place consent path for one already
// trusted device. It reuses the canonical capability proof, the pinned
// identity transport and the signed device authorization; it never issues a
// pairing token or creates a device record.
export async function enableDesktopScope(server: StoredServer, signal?: AbortSignal): Promise<void> {
  const resolved = await resolveStoredServerURL(server);
  const proof: DesktopProofDependencies = {
    fetch: (url, init) => expoFetch(url, init),
    authorization: (purpose) => buildAuthorizationHeader({ daemonId: server.daemonId, purpose }),
    verify: verifyDaemonAssertion,
  };
  await verifyDesktopServer(server, resolved, proof, signal);
  const capability = await fetchDesktopCapability(server, resolved, proof, signal);
  const transport = await desktopGrantTransport(server, resolved, capability);
  try {
    await postDesktopGrant(transport.origin, server, {
      fetch: (url, init) => expoFetch(url, init),
      authorization: () => buildAuthorizationHeader({ daemonId: server.daemonId, purpose: DESKTOP_GRANT_PURPOSE }),
      identity: getOrCreateLocalDeviceIdentity,
      verify: verifyDaemonAssertion,
    }, signal);
  } finally {
    await transport.release();
  }
}
