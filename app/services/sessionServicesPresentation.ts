import type { SessionService } from "./sessionServices";

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
    });
  }

  const groups = [...groupMap.values()].map((group) => ({
    ...group,
    services: [...group.services].sort(compareServices),
  }));

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
  if (!trimmed) {
    return "process";
  }
  return trimmed.length > 72 ? `${trimmed.slice(0, 69)}...` : trimmed;
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