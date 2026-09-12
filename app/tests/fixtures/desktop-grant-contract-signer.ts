// Cross-language contract fixture: signs a real device authorization with the
// app's own payload contract, calls the real daemon HTTP handler, then verifies
// the daemon's signed confirmation with the app's own verifier. Run by
// daemon/server TestDesktopGrantCrossLanguageSignedRoundtrip with isolated test
// keys and servers; it never touches a user daemon, phone or broker.
import { readFileSync } from "node:fs";
import nacl from "tweetnacl";
import { bytesToHex } from "../../services/protocolCrypto";
import { signDeviceAuthorization, verifyDaemonAssertion } from "../../services/deviceAuthContract";
import { DESKTOP_GRANT_PURPOSE } from "../../services/desktopScopeGrantCore";

interface GrantInput {
  url: string;
  daemonId: string;
  daemonPublicKey: string;
  deviceId: string;
  seedHex: string;
}

const input = JSON.parse(readFileSync(process.argv[2], "utf8")) as GrantInput;
const timestamp = Date.now().toString();
const nonceHex = bytesToHex(nacl.randomBytes(16));
const authorization = signDeviceAuthorization({
  purpose: DESKTOP_GRANT_PURPOSE,
  daemonId: input.daemonId,
  deviceId: input.deviceId,
  seedHex: input.seedHex,
  timestamp,
  nonceHex,
});

const response = await fetch(`${input.url.replace(/\/$/, "")}/desktop/scope`, {
  method: "POST",
  headers: { "Content-Type": "application/json", Authorization: authorization },
  body: JSON.stringify({ desktop_scope_version: 1 }),
  tls: { rejectUnauthorized: false },
} as RequestInit & { tls: { rejectUnauthorized: boolean } });
const raw = await response.text();
if (!response.ok) {
  console.error(`grant status=${response.status} body=${raw}`);
  process.exit(1);
}
const payload = JSON.parse(raw) as Record<string, unknown>;
if (payload.ok !== true || payload.device_id !== input.deviceId || payload.desktop_scope_version !== 1) {
  console.error(`grant payload=${raw}`);
  process.exit(2);
}
if (!verifyDaemonAssertion({
  purpose: DESKTOP_GRANT_PURPOSE,
  daemonId: input.daemonId,
  daemonPublicKey: input.daemonPublicKey,
  timestamp: typeof payload.assertion_timestamp === "string" ? payload.assertion_timestamp : null,
  nonceHex: typeof payload.assertion_nonce === "string" ? payload.assertion_nonce : null,
  signatureHex: typeof payload.assertion_signature === "string" ? payload.assertion_signature : null,
})) {
  console.error("daemon confirmation assertion did not verify with the app contract");
  process.exit(3);
}
console.log(JSON.stringify({ ok: true, deviceId: payload.device_id, scopeVersion: payload.desktop_scope_version, purpose: DESKTOP_GRANT_PURPOSE }));
