import {
  buildAuthorizationHeader,
  getOrCreateLocalDeviceIdentity,
  normalizeDaemonId,
  normalizePairingToken,
  normalizePublicKeyHex,
  verifyDaemonAssertion,
} from "./auth";

export interface PairingInput {
  serverUrl: string;
  daemonId?: string;
  daemonPublicKey: string;
  enrollmentToken: string;
}

export async function enrollWithDaemon(input: PairingInput): Promise<{
  daemonId: string;
  daemonPublicKey: string;
}> {
  const daemonId = normalizeDaemonId(input.daemonId);
  const daemonPublicKey = normalizePublicKeyHex(input.daemonPublicKey);
  const enrollmentToken = normalizePairingToken(input.enrollmentToken);
  const pairURL = buildHTTPURL(input.serverUrl, "/pair");

  if (!daemonPublicKey || !enrollmentToken || !pairURL) {
    throw new Error("Invalid pairing link.");
  }

  const identity = await getOrCreateLocalDeviceIdentity();
  const response = await fetch(pairURL, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      enrollment_token: enrollmentToken,
      expected_daemon_id: daemonId || undefined,
      expected_daemon_public_key: daemonPublicKey,
      device_id: identity.deviceId,
      device_name: identity.deviceName,
      device_public_key: identity.publicKeyHex,
    }),
  });

  if (!response.ok) {
    const detail = (await response.text()).trim();
    throw new Error(detail || `Pairing failed (${response.status})`);
  }

  const payload = (await response.json()) as {
    daemon_id?: string;
    daemon_public_key?: string;
    assertion_timestamp?: string;
    assertion_nonce?: string;
    assertion_signature?: string;
  };
  const pairedDaemonId = normalizeDaemonId(payload.daemon_id);
  const pairedDaemonPublicKey = normalizePublicKeyHex(
    payload.daemon_public_key,
  );
  if (!pairedDaemonId || !pairedDaemonPublicKey) {
    throw new Error("Daemon returned an invalid identity.");
  }
  if (
    pairedDaemonPublicKey !== daemonPublicKey ||
    (daemonId && pairedDaemonId !== daemonId)
  ) {
    throw new Error("Pairing target identity did not match the scanned link.");
  }
  if (
    !verifyDaemonAssertion({
      purpose: "zen-pair",
      daemonId: pairedDaemonId,
      daemonPublicKey: pairedDaemonPublicKey,
      timestamp: payload.assertion_timestamp,
      nonceHex: payload.assertion_nonce,
      signatureHex: payload.assertion_signature,
    })
  ) {
    throw new Error("Pairing target failed daemon identity proof.");
  }

  return {
    daemonId: pairedDaemonId,
    daemonPublicKey: pairedDaemonPublicKey,
  };
}

export async function buildSignedRequestHeaders(input: {
  serverUrl: string;
  daemonId: string;
  purpose: "zen-probe" | "zen-upload";
}): Promise<Record<string, string>> {
  return {
    Authorization: await buildAuthorizationHeader({
      daemonId: input.daemonId,
      purpose: input.purpose,
    }),
  };
}

export function buildHTTPURL(
  serverUrl: string,
  pathname: string,
): string | null {
  const trimmed = serverUrl.trim();
  if (!trimmed) return null;

  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol === "ws:") {
      parsed.protocol = "http:";
    } else if (parsed.protocol === "wss:") {
      parsed.protocol = "https:";
    } else if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return null;
    }
    parsed.pathname = pathname;
    parsed.search = "";
    parsed.hash = "";
    return parsed.toString();
  } catch {
    return null;
  }
}
