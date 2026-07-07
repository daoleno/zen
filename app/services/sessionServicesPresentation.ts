import type { SessionService, SessionServiceURL } from "./sessionServices";
import { compactCommandLabel } from "./pathDisplay";

export type DiscoveredSessionService = SessionService & {
  serverId: string;
  serverName: string;
};

export type SessionServiceGroup = {
  key: string;
  project: string;
  serverId: string;
  serverName: string;
  services: DiscoveredSessionService[];
  serverNames: string[];
  portCount: number;
  urlCount: number;
  headerMeta: string;
};

export type SessionServiceSection = {
  key: string;
  title: string;
  groups: SessionServiceGroup[];
};

export function groupSessionServices(
  services: DiscoveredSessionService[],
  options?: { showServerSections?: boolean },
): SessionServiceSection[] {
  const showServerSections = options?.showServerSections ?? false;
  const groupMap = new Map<string, SessionServiceGroup>();

  for (const service of services) {
    const project = serviceProjectLabel(service);
    const groupKey = `${service.serverId}::${project}`;
    const existing = groupMap.get(groupKey);
    if (existing) {
      existing.services.push(service);
      continue;
    }

    groupMap.set(groupKey, {
      key: groupKey,
      project,
      serverId: service.serverId,
      serverName: service.serverName,
      services: [service],
      serverNames: [service.serverName],
      portCount: 1,
      urlCount: 0,
      headerMeta: "",
    });
  }

  const groups = [...groupMap.values()].map((group) => {
    const sortedServices = [...group.services].sort(compareServices);
    const serverNames = uniqueStrings(sortedServices.map((service) => service.serverName));
    const portCount = sortedServices.length;
    const urlCount = uniqueStrings(
      sortedServices.flatMap((service) => (service.urls ?? []).map((item) => item.url)),
    ).length;

    return {
      ...group,
      services: sortedServices,
      serverNames,
      portCount,
      urlCount,
      headerMeta: serviceGroupHeaderMeta({
        serverNames: showServerSections ? [] : serverNames,
        portCount,
        urlCount,
      }),
    };
  });

  groups.sort((left, right) => {
    if (left.serverName !== right.serverName) {
      return left.serverName.localeCompare(right.serverName);
    }
    return left.project.localeCompare(right.project);
  });

  if (!showServerSections) {
    return groups.length > 0 ? [{ key: "all", title: "", groups }] : [];
  }

  const sectionMap = new Map<string, SessionServiceSection>();
  for (const group of groups) {
    const section = sectionMap.get(group.serverId);
    if (section) {
      section.groups.push(group);
      continue;
    }

    sectionMap.set(group.serverId, {
      key: group.serverId,
      title: group.serverName,
      groups: [group],
    });
  }

  return [...sectionMap.values()].sort((left, right) =>
    left.title.localeCompare(right.title),
  );
}

export function serviceProjectLabel(
  service: Pick<DiscoveredSessionService, "project" | "cwd" | "agent_name">,
): string {
  return (
    service.project?.trim() ||
    lastPathSegment(service.cwd) ||
    shortAgentLabel(service.agent_name) ||
    "service"
  );
}

export function shortAgentLabel(value?: string): string {
  const trimmed = value?.trim() || "";
  return trimmed.replace(/\s+\([^)]+\)\s*$/, "") || trimmed;
}

export function shortProcessLabel(value: string): string {
  const trimmed = value.replace(/\s+/g, " ").trim();
  return trimmed || "process";
}

export function serviceAgentLabel(
  service: Pick<DiscoveredSessionService, "agent_name" | "agent_id">,
): string {
  return shortAgentLabel(service.agent_name) || service.agent_id || "agent";
}

export function serviceProcessLabel(
  service: Pick<DiscoveredSessionService, "process" | "command">,
): string {
  return compactCommandLabel(shortProcessLabel(service.process || service.command || ""));
}

export function serviceCommandDetail(
  service: Pick<DiscoveredSessionService, "process" | "command">,
): string {
  const process = (service.process || "").replace(/\s+/g, " ").trim();
  const command = (service.command || "").replace(/\s+/g, " ").trim();
  if (!command || command === process) {
    return "";
  }
  return compactCommandLabel(command);
}

export type PresentedSessionServiceURL = {
  key: string;
  label: string;
  address: string;
  url: string;
};

export function presentSessionServiceURL(
  item: SessionServiceURL,
): PresentedSessionServiceURL {
  return {
    key: item.url || `${item.kind}:${item.address}`,
    label: serviceUrlKindLabel(item.kind || item.label),
    address: item.address || stripUrlScheme(item.url),
    url: item.url,
  };
}

export function serviceBindLabel(
  service: Pick<DiscoveredSessionService, "binds" | "port">,
): string {
  const binds = (service.binds ?? []).map((item) => item.trim()).filter(Boolean);
  if (binds.length === 0) {
    return `localhost:${service.port}`;
  }
  if (binds.length === 1) {
    return `${binds[0]}:${service.port}`;
  }
  return `${binds[0]}:${service.port} +${binds.length - 1}`;
}

function compareServices(
  left: DiscoveredSessionService,
  right: DiscoveredSessionService,
): number {
  if (left.port !== right.port) {
    return left.port - right.port;
  }
  const leftProcess = shortProcessLabel(left.process || left.command || "");
  const rightProcess = shortProcessLabel(right.process || right.command || "");
  if (leftProcess !== rightProcess) {
    return leftProcess.localeCompare(rightProcess);
  }
  return shortAgentLabel(left.agent_name).localeCompare(
    shortAgentLabel(right.agent_name),
  );
}

function lastPathSegment(value?: string): string {
  const trimmed = value?.trim().replace(/\/+$/, "") || "";
  if (!trimmed || trimmed === "/") {
    return trimmed;
  }

  const parts = trimmed.split("/").filter(Boolean);
  return parts[parts.length - 1] || trimmed;
}

function serviceGroupHeaderMeta({
  serverNames,
  portCount,
  urlCount,
}: {
  serverNames: string[];
  portCount: number;
  urlCount: number;
}): string {
  const serverLabel = serverNames.length > 1
    ? `${serverNames[0]} +${serverNames.length - 1}`
    : serverNames[0] || "server";
  return [
    serverNames.length > 0 ? serverLabel : "",
    `${portCount} port${portCount === 1 ? "" : "s"}`,
    `${urlCount} URL${urlCount === 1 ? "" : "s"}`,
  ].filter(Boolean).join(" · ");
}

function serviceUrlKindLabel(value?: string): string {
  const normalized = (value || "").trim().toLowerCase();
  if (!normalized) {
    return "URL";
  }
  if (normalized === "lan") {
    return "LAN";
  }
  if (normalized === "tailscale") {
    return "Tailscale";
  }
  if (normalized === "local" || normalized === "localhost") {
    return "Local";
  }
  return normalized
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function stripUrlScheme(value: string) {
  return value.replace(/^[a-z][a-z0-9+.-]*:\/\//i, "");
}

function uniqueStrings(values: string[]): string[] {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const value of values) {
    const trimmed = value.trim();
    if (!trimmed || seen.has(trimmed)) {
      continue;
    }
    seen.add(trimmed);
    result.push(trimmed);
  }
  return result;
}
