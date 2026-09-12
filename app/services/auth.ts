import * as Crypto from "expo-crypto";
import * as Device from "expo-device";
import * as SecureStore from "expo-secure-store";
import AsyncStorage from "@react-native-async-storage/async-storage";
import { Platform } from "react-native";
import nacl from "tweetnacl";
import { bytesToHex, hexToBytes, normalizeFixedHex } from "./protocolCrypto";
import { normalizeDaemonId, signDeviceAuthorization } from "./deviceAuthContract";

export {
  bytesToHex,
  hexToBytes,
  verifyLinkPairingSignature,
} from "./protocolCrypto";
export {
  buildServerAssertionPayload,
  buildSignaturePayload,
  normalizeDaemonId,
  normalizePublicKeyHex,
  verifyDaemonAssertion,
} from "./deviceAuthContract";
export type { DaemonAssertionInput } from "./deviceAuthContract";

const DEVICE_ID_KEY = "zen.device.v3.id";
const DEVICE_NAME_KEY = "zen.device.v3.name";
const DEVICE_SEED_KEY = "zen.device.v3.seed";
const DEVICE_PUBLIC_KEY_KEY = "zen.device.v3.public-key";
const WEB_SECURE_STORE_PREFIX = "zen:secure:";

export interface LocalDeviceIdentity {
  deviceId: string;
  deviceName: string;
  publicKeyHex: string;
  seedHex: string;
}

export type AuthPurpose =
  | "zen-desktop"
  | "zen-desktop-capability"
  | "zen-device-admin:desktop-grant:POST:/desktop/scope"
  | "zen-connect"
  | "zen-upload"
  | "zen-probe"
  | "zen-session-file";

export function normalizePairingToken(
  rawValue: string | null | undefined,
): string {
  return normalizeFixedHex(rawValue, 64);
}

export async function getOrCreateLocalDeviceIdentity(): Promise<LocalDeviceIdentity> {
  const [storedDeviceId, storedName, storedSeedHex, storedPublicKeyHex] =
    await Promise.all([
      getSecureItem(DEVICE_ID_KEY),
      getSecureItem(DEVICE_NAME_KEY),
      getSecureItem(DEVICE_SEED_KEY),
      getSecureItem(DEVICE_PUBLIC_KEY_KEY),
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
    setSecureItem(DEVICE_ID_KEY, nextIdentity.deviceId),
    setSecureItem(DEVICE_NAME_KEY, nextIdentity.deviceName),
    setSecureItem(DEVICE_SEED_KEY, nextIdentity.seedHex),
    setSecureItem(DEVICE_PUBLIC_KEY_KEY, nextIdentity.publicKeyHex),
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
  return signDeviceAuthorization({
    purpose: input.purpose,
    daemonId,
    deviceId: identity.deviceId,
    seedHex: identity.seedHex,
    timestamp,
    nonceHex,
  });
}

function defaultDeviceName(): string {
  return Device.deviceName?.trim() || Device.modelName?.trim() || "Zen mobile";
}

async function getSecureItem(key: string): Promise<string | null> {
  if (Platform.OS === "web") {
    return AsyncStorage.getItem(`${WEB_SECURE_STORE_PREFIX}${key}`);
  }
  return SecureStore.getItemAsync(key);
}

async function setSecureItem(key: string, value: string): Promise<void> {
  if (Platform.OS === "web") {
    await AsyncStorage.setItem(`${WEB_SECURE_STORE_PREFIX}${key}`, value);
    return;
  }
  await SecureStore.setItemAsync(key, value);
}
