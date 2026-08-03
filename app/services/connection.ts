import {
  bytesToHex,
  normalizeFixedHex,
  verifyLinkPairingSignature,
} from "./protocolCrypto";

export interface LinkTransportCandidate {
  name: string;
  admissionUrl?: string;
  url: string;
}

export interface LinkTransportEnrollment {
  kind: "link";
  routeId: string;
  transportPin: string;
  candidates: LinkTransportCandidate[];
}

export interface ConnectionImportPayload {
  url: string;
  name?: string;
  daemonId?: string;
  daemonPublicKey: string;
  enrollmentToken: string;
  link?: LinkTransportEnrollment;
}

const CONNECT_PARAM_ALIASES = {
  url: ["u", "url"],
  name: ["n", "name"],
  daemonId: ["d", "daemon_id"],
  daemonPublicKey: ["k", "daemon_public_key"],
  enrollmentToken: ["t", "enrollment_token"],
} as const;
const CONNECT_PARAM_PAYLOAD = "p";
const CONNECT_PAYLOAD_VERSION = 1;
const CONNECT_PUBLIC_KEY_BYTES = 32;
const CONNECT_TOKEN_BYTES = 32;
const CONNECT_PAYLOAD_MIN_LENGTH =
  1 + 2 + CONNECT_PUBLIC_KEY_BYTES + CONNECT_TOKEN_BYTES;
const MAX_CONNECT_PAYLOAD_CHARACTERS = 64 << 10;
const MAX_LINK_CANDIDATES = 16;

const normalizeDaemonId = (value: string | null | undefined) =>
  normalizeFixedHex(value, 64);
const normalizePairingToken = (value: string | null | undefined) =>
  normalizeFixedHex(value, 64);
const normalizePublicKeyHex = (value: string | null | undefined) =>
  normalizeFixedHex(value, 64);

export function normalizeServerURL(rawValue: string): string {
  const trimmed = rawValue.trim();
  if (!trimmed) return "";

  try {
    const parsed = new URL(trimmed);
    switch (parsed.protocol) {
      case "http:":
        parsed.protocol = "ws:";
        break;
      case "https:":
        parsed.protocol = "wss:";
        break;
      case "ws:":
      case "wss:":
        break;
      default:
        return "";
    }

    if (parsed.pathname === "" || parsed.pathname === "/") {
      parsed.pathname = "/ws";
    }

    parsed.search = "";
    parsed.hash = "";
    return parsed.toString();
  } catch {
    return "";
  }
}

export function parseConnectLink(
  rawValue: string,
): ConnectionImportPayload | null {
  const trimmed = rawValue.trim();
  if (!trimmed) return null;

  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol !== "zen:") {
      return null;
    }

    const compactPayload = parsed.searchParams
      .get(CONNECT_PARAM_PAYLOAD)
      ?.trim();
    if (compactPayload) {
      if (parsed.searchParams.get("v")?.trim() === "2") {
        return parseLinkPairingPayload(compactPayload);
      }
      return parseCompactConnectPayload(compactPayload);
    }

    const url = normalizeServerURL(readConnectLinkParam(parsed, "url") || "");
    if (!url) return null;

    const payload: ConnectionImportPayload = {
      url,
      name: readConnectLinkParam(parsed, "name")?.trim() || undefined,
      daemonId:
        normalizeDaemonId(readConnectLinkParam(parsed, "daemonId")) ||
        undefined,
      daemonPublicKey: normalizePublicKeyHex(
        readConnectLinkParam(parsed, "daemonPublicKey"),
      ),
      enrollmentToken: normalizePairingToken(
        readConnectLinkParam(parsed, "enrollmentToken"),
      ),
    };
    return payload.daemonPublicKey && payload.enrollmentToken ? payload : null;
  } catch {
    return null;
  }
}

interface CompactLinkPairingPayload {
  v?: unknown;
  d?: unknown;
  k?: unknown;
  e?: unknown;
  r?: unknown;
  p?: unknown;
  c?: unknown;
  x?: unknown;
  z?: unknown;
}

function parseLinkPairingPayload(
  encodedPayload: string,
): ConnectionImportPayload | null {
  try {
    const decoded = decodeBase64URL(encodedPayload);
    const raw = JSON.parse(
      new TextDecoder().decode(decoded),
    ) as CompactLinkPairingPayload;
    if (
      raw.v !== 2 ||
      typeof raw.d !== "string" ||
      typeof raw.k !== "string" ||
      typeof raw.e !== "string" ||
      typeof raw.r !== "string" ||
      typeof raw.p !== "string" ||
      !Array.isArray(raw.c) ||
      raw.c.length === 0 ||
      raw.c.length > MAX_LINK_CANDIDATES ||
      typeof raw.x !== "number" ||
      typeof raw.z !== "string"
    ) {
      return null;
    }

    const daemonId = normalizeDaemonId(raw.d);
    const daemonPublicKey = normalizePublicKeyHex(raw.k);
    const enrollmentToken = normalizePairingToken(raw.e);
    const routeId = normalizeFixedHex(raw.r, 32);
    const transportPin = normalizeFixedHex(raw.p, 64);
    const signature = normalizeFixedHex(raw.z, 128);
    if (
      !daemonId ||
      !daemonPublicKey ||
      !enrollmentToken ||
      !routeId ||
      !transportPin ||
      !signature ||
      !Number.isSafeInteger(raw.x) ||
      raw.x <= Date.now()
    ) {
      return null;
    }

    const candidates: LinkTransportCandidate[] = [];
    const bindingCandidateValues: string[] = [];
    for (const value of raw.c) {
      if (!value || typeof value !== "object") {
        return null;
      }
      const candidate = value as Record<string, unknown>;
      const name = typeof candidate.n === "string" ? candidate.n.trim() : "";
      const admissionUrl =
        typeof candidate.a === "string" ? normalizeServerURL(candidate.a) : "";
      const url =
        typeof candidate.s === "string" ? normalizeServerURL(candidate.s) : "";
      if (!name || !url) {
        return null;
      }
      candidates.push({
        name,
        admissionUrl: admissionUrl || undefined,
        url,
      });
      bindingCandidateValues.push(
        name,
        typeof candidate.a === "string" ? candidate.a.trim() : "",
        typeof candidate.s === "string" ? candidate.s.trim() : "",
      );
    }
    const admissionCandidate = candidates.find(
      (candidate) => candidate.admissionUrl,
    );
    if (!admissionCandidate?.admissionUrl) {
      return null;
    }
    const bindingPayload = new TextEncoder().encode(
      [
        "2",
        daemonId,
        daemonPublicKey,
        enrollmentToken,
        routeId,
        transportPin,
        raw.x.toString(),
        ...bindingCandidateValues,
      ].join("\n"),
    );
    if (
      !verifyLinkPairingSignature({
        daemonPublicKey,
        bindingPayload,
        signatureHex: signature,
      })
    ) {
      return null;
    }

    return {
      url: admissionCandidate.admissionUrl,
      daemonId,
      daemonPublicKey,
      enrollmentToken,
      link: {
        kind: "link",
        routeId,
        transportPin,
        candidates,
      },
    };
  } catch {
    return null;
  }
}

function parseCompactConnectPayload(
  encodedPayload: string,
): ConnectionImportPayload | null {
  try {
    const payload = decodeBase64URL(encodedPayload);
    if (payload.length < CONNECT_PAYLOAD_MIN_LENGTH) {
      return null;
    }
    if (payload[0] !== CONNECT_PAYLOAD_VERSION) {
      return null;
    }

    const urlLength = (payload[1] << 8) | payload[2];
    const expectedLength = CONNECT_PAYLOAD_MIN_LENGTH + urlLength;
    if (payload.length !== expectedLength) {
      return null;
    }

    let offset = 3;
    const url = normalizeServerURL(
      new TextDecoder().decode(payload.slice(offset, offset + urlLength)),
    );
    offset += urlLength;

    const daemonPublicKey = normalizePublicKeyHex(
      bytesToHex(payload.slice(offset, offset + CONNECT_PUBLIC_KEY_BYTES)),
    );
    offset += CONNECT_PUBLIC_KEY_BYTES;

    const enrollmentToken = normalizePairingToken(
      bytesToHex(payload.slice(offset, offset + CONNECT_TOKEN_BYTES)),
    );

    if (!url || !daemonPublicKey || !enrollmentToken) {
      return null;
    }

    return {
      url,
      daemonPublicKey,
      enrollmentToken,
    };
  } catch {
    return null;
  }
}

function decodeBase64URL(value: string): Uint8Array {
  const sanitized = value.trim();
  if (
    !sanitized ||
    sanitized.length > MAX_CONNECT_PAYLOAD_CHARACTERS ||
    /[^A-Za-z0-9\-_]/.test(sanitized)
  ) {
    throw new Error("Invalid base64url payload.");
  }

  const remainder = sanitized.length % 4;
  if (remainder === 1) {
    throw new Error("Invalid base64url length.");
  }

  const padded =
    remainder === 0 ? sanitized : sanitized + "=".repeat(4 - remainder);
  const outputLength =
    Math.floor((padded.length * 3) / 4) -
    (padded.endsWith("==") ? 2 : padded.endsWith("=") ? 1 : 0);

  const output = new Uint8Array(outputLength);
  let outputOffset = 0;

  for (let index = 0; index < padded.length; index += 4) {
    const chunk0 = decodeBase64URLChar(padded[index]);
    const chunk1 = decodeBase64URLChar(padded[index + 1]);
    const chunk2 =
      padded[index + 2] === "=" ? 0 : decodeBase64URLChar(padded[index + 2]);
    const chunk3 =
      padded[index + 3] === "=" ? 0 : decodeBase64URLChar(padded[index + 3]);
    const combined = (chunk0 << 18) | (chunk1 << 12) | (chunk2 << 6) | chunk3;

    output[outputOffset++] = (combined >> 16) & 0xff;
    if (padded[index + 2] !== "=" && outputOffset < output.length) {
      output[outputOffset++] = (combined >> 8) & 0xff;
    }
    if (padded[index + 3] !== "=" && outputOffset < output.length) {
      output[outputOffset++] = combined & 0xff;
    }
  }

  return output;
}

function decodeBase64URLChar(value: string): number {
  const code = value.charCodeAt(0);
  if (code >= 65 && code <= 90) {
    return code - 65;
  }
  if (code >= 97 && code <= 122) {
    return code - 71;
  }
  if (code >= 48 && code <= 57) {
    return code + 4;
  }
  if (value === "-") {
    return 62;
  }
  if (value === "_") {
    return 63;
  }
  throw new Error("Invalid base64url character.");
}

function readConnectLinkParam(
  parsed: URL,
  key: keyof typeof CONNECT_PARAM_ALIASES,
): string | null {
  for (const alias of CONNECT_PARAM_ALIASES[key]) {
    const value = parsed.searchParams.get(alias)?.trim();
    if (value) {
      return value;
    }
  }
  return null;
}
