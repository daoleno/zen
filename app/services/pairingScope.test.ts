import { expect, test } from "bun:test";
import nacl from "tweetnacl";
import { DESKTOP_SCOPE_VERSION, PAIRING_SCOPE_COPY, signPairingScope } from "./pairingScope";
import { bytesToHex, hexToBytes } from "./protocolCrypto";

const seed = new Uint8Array(32).fill(9);
const key = nacl.sign.keyPair.fromSeed(seed);
const input = { daemonPublicKey: "1".repeat(64), enrollmentToken: "2".repeat(64), deviceId: "phone", publicKeyHex: bytesToHex(key.publicKey), seedHex: bytesToHex(seed) };

test("default pairing scope acknowledgment binds host, token, device ID and key", () => {
  expect(DESKTOP_SCOPE_VERSION).toBe(1);
  const signature = hexToBytes(signPairingScope(input));
  const fields = ["zen-pair-desktop-scope-v1", input.daemonPublicKey, input.enrollmentToken, input.deviceId, input.publicKeyHex];
  const verify = (parts: string[]) => nacl.sign.detached.verify(new TextEncoder().encode(parts.join("\n")), signature, key.publicKey);
  expect(verify(fields)).toBe(true);
  for (let index = 0; index < fields.length; index++) {
    const changed = [...fields]; changed[index] += "x";
    expect(verify(changed)).toBe(false);
  }
});
test("invalid and mismatched identity cannot acknowledge expanded scope", () => {
  for (const changed of [{ deviceId: "phone\nother" }, { seedHex: "" }, { publicKeyHex: "0".repeat(64) }, { enrollmentToken: "" }]) {
    expect(() => signPairingScope({ ...input, ...changed })).toThrow();
  }
});
test("one-time pairing copy names unattended, OS login and confidentiality limits", () => {
  for (const term of ["terminal", "unattended", "lock", "OS login", "OS permissions", "encrypted", "existing device", "logged-in session"]) expect(PAIRING_SCOPE_COPY).toContain(term);
});
