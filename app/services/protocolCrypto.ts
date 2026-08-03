import nacl from "tweetnacl";

export function verifyLinkPairingSignature(input: {
  daemonPublicKey: string;
  bindingPayload: Uint8Array;
  signatureHex: string;
}): boolean {
  const daemonPublicKey = normalizeFixedHex(input.daemonPublicKey, 64);
  const signatureHex = normalizeFixedHex(input.signatureHex, 128);
  if (!daemonPublicKey || !signatureHex) {
    return false;
  }
  try {
    const domain = new TextEncoder().encode("zen-link-pairing-v2\u0000");
    const signed = new Uint8Array(domain.length + input.bindingPayload.length);
    signed.set(domain);
    signed.set(input.bindingPayload, domain.length);
    return nacl.sign.detached.verify(
      signed,
      hexToBytes(signatureHex),
      hexToBytes(daemonPublicKey),
    );
  } catch {
    return false;
  }
}

export function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join(
    "",
  );
}

export function hexToBytes(value: string): Uint8Array {
  const normalized = value.trim().toLowerCase();
  if (!/^[0-9a-f]+$/.test(normalized) || normalized.length % 2 !== 0) {
    throw new Error("Invalid hex payload.");
  }
  const output = new Uint8Array(normalized.length / 2);
  for (let index = 0; index < normalized.length; index += 2) {
    output[index / 2] = Number.parseInt(normalized.slice(index, index + 2), 16);
  }
  return output;
}

export function normalizeFixedHex(
  rawValue: string | null | undefined,
  expectedLength: number,
): string {
  const trimmed = rawValue?.trim().toLowerCase() || "";
  if (!trimmed) return "";
  if (!new RegExp(`^[0-9a-f]{${expectedLength}}$`).test(trimmed)) {
    return "";
  }
  return trimmed;
}
