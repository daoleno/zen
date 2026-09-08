import ipaddr from "ipaddr.js";
import type { StoredServer } from "./storedServerContract";

export interface DesktopLanConsent {
  origin: string;
  daemonId: string;
  daemonPublicKey: string;
  acknowledgedAt: number;
}

export function isPrivateDesktopHost(host: string): boolean {
  const literal = host.startsWith("[") && host.endsWith("]") ? host.slice(1, -1) : host;
  if (literal.includes("%") || !ipaddr.isValid(literal)) return false;
  const address = ipaddr.process(literal);
  return address.kind() === "ipv4"
    ? ["private", "carrierGradeNat"].includes(address.range())
    : address.range() === "uniqueLocal";
}

function endpoint(value: string): URL {
  const url = new URL(value);
  if (!["ws:", "wss:"].includes(url.protocol) || url.username || url.password || (url.port && Number(url.port) < 1)) {
    throw new Error("Invalid desktop endpoint. URL credentials are not supported.");
  }
  return url;
}

export function desktopLanOrigin(server: Pick<StoredServer, "url" | "transportKind">): string | null {
  try {
    const url = endpoint(server.url);
    return server.transportKind !== "link" && url.protocol === "ws:" && isPrivateDesktopHost(url.hostname)
      ? url.origin : null;
  } catch { return null; }
}

export function normalizeDesktopLanConsent(value: unknown, server: Pick<StoredServer, "url" | "transportKind" | "daemonId" | "daemonPublicKey">): DesktopLanConsent | undefined {
  if (!value || typeof value !== "object") return undefined;
  const approval = value as Partial<DesktopLanConsent>;
  const origin = desktopLanOrigin(server);
  if (!origin || approval.origin !== origin || approval.daemonId !== server.daemonId ||
      approval.daemonPublicKey !== server.daemonPublicKey || typeof approval.acknowledgedAt !== "number" ||
      !Number.isFinite(approval.acknowledgedAt) || approval.acknowledgedAt <= 0) return undefined;
  return { origin, daemonId: server.daemonId, daemonPublicKey: server.daemonPublicKey, acknowledgedAt: approval.acknowledgedAt };
}

export function hasDesktopLanConsent(server: StoredServer): boolean {
  return !!normalizeDesktopLanConsent(server.desktopLanConsent, server);
}

export function desktopTransportPlan(server: StoredServer, resolved: string) {
  const source = endpoint(server.url);
  const target = endpoint(resolved);
  let transport: "tls" | "pinned-link" | "trusted-lan";
  if (server.transportKind === "link") {
    if (source.protocol !== "wss:" || target.protocol !== "ws:" || target.hostname !== "127.0.0.1" || !target.port ||
        !/^[0-9a-f]{64}$/i.test(server.transportPin || "") || !/^[0-9a-f]{32}$/i.test(server.linkRouteId || "")) {
      throw new Error("The pinned Zen Link desktop transport is not available.");
    }
    transport = "pinned-link";
  } else if (source.protocol === "wss:" && target.protocol === "wss:" && source.origin === target.origin) {
    transport = "tls";
  } else if (source.protocol === "ws:" && source.origin === target.origin && desktopLanOrigin(server)) {
    if (!hasDesktopLanConsent(server)) throw new Error("Review and allow unencrypted desktop access for this paired LAN server first.");
    transport = "trusted-lan";
  } else {
    throw new Error("Use a secure endpoint or a numeric private-network address for desktop access. Secure connections are never downgraded.");
  }
  target.pathname = "/desktop"; target.search = ""; target.hash = "";
  return { url: target.toString(), transport, boundOrigin: target.origin, sourceOrigin: source.origin,
    transportPin: transport === "pinned-link" ? server.transportPin! : "" };
}
