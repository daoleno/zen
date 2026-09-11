import nacl from "tweetnacl";
import { bytesToHex, hexToBytes, normalizeFixedHex } from "./protocolCrypto";

export const DESKTOP_SCOPE_VERSION = 1;
export const PAIRING_SCOPE_COPY = "Pairing grants this phone terminal access and unattended desktop viewing and control of the current logged-in session, including supported lock and OS login screens after one host install. OS permissions are still required for lock and login after reboot. Password entry requires an encrypted connection. Re-pairing confirms this access for an existing device.";

export function signPairingScope(input: {
  daemonPublicKey: string;
  enrollmentToken: string;
  deviceId: string;
  publicKeyHex: string;
  seedHex: string;
}): string {
  const host = normalizeFixedHex(input.daemonPublicKey, 64);
  const token = normalizeFixedHex(input.enrollmentToken, 64);
  const key = normalizeFixedHex(input.publicKeyHex, 64);
  const seed = normalizeFixedHex(input.seedHex, 64);
  const id = input.deviceId.trim();
  if (!host || !token || !key || !seed || !id || /[\r\n]/.test(id)) throw new Error("Invalid pairing scope identity.");
  const pair = nacl.sign.keyPair.fromSeed(hexToBytes(seed));
  if (bytesToHex(pair.publicKey) !== key) throw new Error("Pairing device key mismatch.");
  const payload = new TextEncoder().encode(["zen-pair-desktop-scope-v1", host, token, id, key].join("\n"));
  return bytesToHex(nacl.sign.detached(payload, pair.secretKey));
}

export class PairingCancelledError extends Error {
  constructor() { super("Pairing cancelled."); this.name = "PairingCancelledError"; }
}
