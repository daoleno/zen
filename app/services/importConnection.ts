import { parseConnectLink } from "./connection";
import {
  markOnboarded,
  saveServer,
  setServerAutoConnect,
  type StoredServer,
} from "./storage";
import { enrollWithDaemon } from "./pairing";
import {
  releasePairingLinkTransport,
  resolvePairingLinkURL,
} from "./pinnedTransport";

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

  const pairingURL = payload.link
    ? await resolvePairingLinkURL({
        routeId: payload.link.routeId,
        transportPin: payload.link.transportPin,
        url: payload.url,
      })
    : payload.url;
  let pairing: Awaited<ReturnType<typeof enrollWithDaemon>>;
  try {
    pairing = await enrollWithDaemon({
      serverUrl: pairingURL,
      daemonId: payload.daemonId,
      daemonPublicKey: payload.daemonPublicKey,
      enrollmentToken: payload.enrollmentToken,
    });
  } finally {
    if (payload.link) {
      await releasePairingLinkTransport(payload.link.routeId);
    }
  }

  const primaryStableURL =
    payload.link?.candidates.find(
      (candidate) => candidate.admissionUrl === payload.url,
    )?.url ||
    payload.link?.candidates[0]?.url ||
    payload.url;
  const savedServer = await saveServer({
    name: payload.name || "",
    url: primaryStableURL,
    daemonId: pairing.daemonId,
    daemonPublicKey: pairing.daemonPublicKey,
    transportKind: payload.link ? "link" : "manual",
    transportPin: payload.link?.transportPin,
    linkRouteId: payload.link?.routeId,
    transportCandidates: payload.link?.candidates.map((candidate) => ({
      name: candidate.name,
      kind: "link" as const,
      url: candidate.url,
    })),
  });

  await markOnboarded();
  await setServerAutoConnect(savedServer.id, true);
  await options.onImported?.(savedServer);
  return savedServer;
}
