import * as Crypto from "expo-crypto";
import * as Device from "expo-device";
import * as SecureStore from "expo-secure-store";
import nacl from "tweetnacl";

const DEVICE_ID_KEY = "zen.device.v3.id";
const DEVICE_NAME_KEY = "zen.device.v3.name";
const DEVICE_SEED_KEY = "zen.device.v3.seed";
const DEVICE_PUBLIC_KEY_KEY = "zen.device.v3.public-key";
const AUTH_HEADER_PREFIX = "ZenDevice ";

export interface LocalDeviceIdentity {
  deviceId: string;
  deviceName: string;
  publicKeyHex: string;
  seedHex: string;
}

export interface DaemonAssertionInput {
  purpose: string;
  daemonId: string;
  daemonPublicKey: string;
  timestamp: string | null | undefined;
  nonceHex: string | null | undefined;
  signatureHex: string | null | undefined;
}

export type AuthPurpose = "zen-connect" | "zen-upload" | "zen-probe";

export function normalizeDaemonId(rawValue: string | null | undefined): string {
  return normalizeFixedHex(rawValue, 64);
}

export function normalizePublicKeyHex(
  rawValue: string | null | undefined,
): string {
  return normalizeFixedHex(rawValue, 64);
}

export function normalizePairingToken(
  rawValue: string | null | undefined,
): string {
  return normalizeFixedHex(rawValue, 64);
}

export async function getOrCreateLocalDeviceIdentity(): Promise<LocalDeviceIdentity> {
  const [storedDeviceId, storedName, storedSeedHex, storedPublicKeyHex] =
    await Promise.all([
      SecureStore.getItemAsync(DEVICE_ID_KEY),
      SecureStore.getItemAsync(DEVICE_NAME_KEY),
      SecureStore.getItemAsync(DEVICE_SEED_KEY),
      SecureStore.getItemAsync(DEVICE_PUBLIC_KEY_KEY),
    ]);

  const normalizedSeedHex = normalizeFixedHex(storedSeedHex, 64);
  const normalizedPublicKeyHex = normalizeFixedHex(storedPublicKeyHex, 64);
  if (storedDeviceId?.trim() && normalizedSeedHex && normalizedPublicKeyHex) {
    return {
      deviceId: storedDeviceId.trim(),
      deviceName: storedName?.trim() || defaultDeviceName(),
      seedHex: normalizedSeedHex,
      publicKeyHex: normalizedPublicKeyHex,
    };
  }

  const seed = Crypto.getRandomBytes(32);
  const keyPair = nacl.sign.keyPair.fromSeed(seed);
  const nextIdentity: LocalDeviceIdentity = {
    deviceId: Crypto.randomUUID(),
    deviceName: defaultDeviceName(),
    seedHex: bytesToHex(seed),
    publicKeyHex: bytesToHex(keyPair.publicKey),
  };

  await Promise.all([
    SecureStore.setItemAsync(DEVICE_ID_KEY, nextIdentity.deviceId),
    SecureStore.setItemAsync(DEVICE_NAME_KEY, nextIdentity.deviceName),
    SecureStore.setItemAsync(DEVICE_SEED_KEY, nextIdentity.seedHex),
    SecureStore.setItemAsync(DEVICE_PUBLIC_KEY_KEY, nextIdentity.publicKeyHex),
  ]);

  return nextIdentity;
}

export async function buildAuthorizationHeader(input: {
  daemonId: string;
  purpose: AuthPurpose;
}): Promise<string> {
  const daemonId = normalizeDaemonId(input.daemonId);
  if (!daemonId) {
    throw new Error("Missing daemon identity.");
  }

  const identity = await getOrCreateLocalDeviceIdentity();
  const timestamp = Date.now().toString();
  const nonceHex = bytesToHex(Crypto.getRandomBytes(16));
  const payload = buildSignaturePayload(
    input.purpose,
    daemonId,
    identity.deviceId,
    timestamp,
    nonceHex,
  );

  const keyPair = nacl.sign.keyPair.fromSeed(hexToBytes(identity.seedHex));
  const signature = nacl.sign.detached(payload, keyPair.secretKey);

  return `${AUTH_HEADER_PREFIX}v1:${identity.deviceId}:${daemonId}:${timestamp}:${nonceHex}:${bytesToHex(signature)}`;
}

export function buildSignaturePayload(
  purpose: AuthPurpose,
  daemonId: string,
  deviceId: string,
  timestamp: string,
  nonceHex: string,
): Uint8Array {
  const encoder = new TextEncoder();
  return encoder.encode(
    [
      purpose.trim(),
      normalizeDaemonId(daemonId),
      deviceId.trim(),
      timestamp.trim(),
      normalizeFixedHex(nonceHex, 32),
    ].join("\n"),
  );
}

export function buildServerAssertionPayload(
  purpose: string,
  daemonId: string,
  timestamp: string,
  nonceHex: string,
): Uint8Array {
  const encoder = new TextEncoder();
  return encoder.encode(
    [
      purpose.trim(),
      normalizeDaemonId(daemonId),
      timestamp.trim(),
      normalizeFixedHex(nonceHex, 32),
    ].join("\n"),
  );
}

export function verifyDaemonAssertion(input: DaemonAssertionInput): boolean {
  const daemonId = normalizeDaemonId(input.daemonId);
  const daemonPublicKey = normalizePublicKeyHex(input.daemonPublicKey);
  const nonceHex = normalizeFixedHex(input.nonceHex, 32);
  const signatureHex = normalizeFixedHex(input.signatureHex, 128);
  const timestamp = input.timestamp?.trim() || "";

  if (
    !input.purpose.trim() ||
    !daemonId ||
    !daemonPublicKey ||
    !timestamp ||
    !nonceHex ||
    !signatureHex
  ) {
    return false;
  }

  try {
    return nacl.sign.detached.verify(
      buildServerAssertionPayload(input.purpose, daemonId, timestamp, nonceHex),
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

function normalizeFixedHex(
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

function defaultDeviceName(): string {
  return Device.deviceName?.trim() || Device.modelName?.trim() || "zen mobile";
}
