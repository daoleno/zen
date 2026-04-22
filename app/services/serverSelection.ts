import type { Agent, ConnectionState } from "../store/agents";
import type { ServerLatencySample } from "./serverLatency";
import type { StoredServer } from "./storage";

export interface AgentDirectorySection {
  key: string;
  title: string;
  subtitle: string;
  data: Agent[];
}

export function buildServerIdentityKey(
  server: Pick<StoredServer, "daemonId" | "daemonPublicKey">,
): string {
  return `${server.daemonId.trim()}::${server.daemonPublicKey.trim()}`;
}

export function resolvePreferredServerIdByServerId(input: {
  servers: StoredServer[];
  connectionStates: Record<string, ConnectionState>;
  latencyById: Record<string, ServerLatencySample | undefined>;
}): Record<string, string> {
  const groups = new Map<string, StoredServer[]>();
  const positionById = new Map(
    input.servers.map((server, index) => [server.id, index]),
  );

  for (const server of input.servers) {
    const identityKey = buildServerIdentityKey(server);
    const group = groups.get(identityKey);
    if (group) {
      group.push(server);
    } else {
      groups.set(identityKey, [server]);
    }
  }

  const preferredByServerId: Record<string, string> = {};

  for (const group of groups.values()) {
    const preferred = [...group].sort((left, right) => {
      const connectionDelta =
        connectionPriority(input.connectionStates[left.id]) -
        connectionPriority(input.connectionStates[right.id]);
      if (connectionDelta !== 0) {
        return connectionDelta;
      }

      const latencyDelta =
        readLatency(input.latencyById[left.id]) -
        readLatency(input.latencyById[right.id]);
      if (latencyDelta !== 0) {
        return latencyDelta;
      }

      const freshnessDelta =
        readMeasuredAt(input.latencyById[right.id]) -
        readMeasuredAt(input.latencyById[left.id]);
      if (freshnessDelta !== 0) {
        return freshnessDelta;
      }

      const positionDelta =
        (positionById.get(left.id) ?? Number.MAX_SAFE_INTEGER) -
        (positionById.get(right.id) ?? Number.MAX_SAFE_INTEGER);
      if (positionDelta !== 0) {
        return positionDelta;
      }

      return (
        left.name.localeCompare(right.name) ||
        left.url.localeCompare(right.url) ||
        left.id.localeCompare(right.id)
      );
    })[0];

    for (const server of group) {
      preferredByServerId[server.id] = preferred.id;
    }
  }

  return preferredByServerId;
}

export function filterAgentsByPreferredServers(input: {
  agents: Agent[];
  servers: StoredServer[];
  connectionStates: Record<string, ConnectionState>;
  latencyById: Record<string, ServerLatencySample | undefined>;
}): Agent[] {
  const knownServerIds = new Set(input.servers.map((server) => server.id));
  const preferredByServerId = resolvePreferredServerIdByServerId(input);

  return input.agents.filter((agent) => {
    if (!knownServerIds.has(agent.serverId)) {
      return true;
    }
    return preferredByServerId[agent.serverId] === agent.serverId;
  });
}

export function groupAgentsByDirectory(
  agents: Agent[],
  options: {
    showServerName?: boolean;
  } = {},
): AgentDirectorySection[] {
  const sections: AgentDirectorySection[] = [];
  const sectionByKey = new Map<string, AgentDirectorySection>();

  for (const agent of agents) {
    const directory = normalizeDirectory(agent.cwd);
    const project = normalize(agent.project);
    const serverName = normalize(agent.serverName);

    let key = "";
    let title = "";
    let subtitle = "";

    if (directory) {
      key = `${agent.serverId}::cwd::${directory}`;
      title = basename(directory);
      subtitle = options.showServerName && serverName
        ? `${serverName} · ${directory}`
        : directory;
    } else if (project) {
      key = `${agent.serverId}::project::${project}`;
      title = project;
      subtitle = options.showServerName && serverName
        ? `${serverName} · Project`
        : "Project";
    } else {
      key = `${agent.serverId}::fallback`;
      title = serverName || "Unknown workspace";
      subtitle = options.showServerName && serverName
        ? `${serverName} · No directory`
        : "No directory";
    }

    const existing = sectionByKey.get(key);
    if (existing) {
      existing.data.push(agent);
      continue;
    }

    const nextSection: AgentDirectorySection = {
      key,
      title,
      subtitle,
      data: [agent],
    };
    sectionByKey.set(key, nextSection);
    sections.push(nextSection);
  }

  return sections;
}

function connectionPriority(state: ConnectionState | undefined): number {
  switch (state) {
    case "connected":
      return 0;
    case "connecting":
      return 1;
    default:
      return 2;
  }
}

function readLatency(sample?: ServerLatencySample): number {
  return sample?.latencyMs ?? Number.POSITIVE_INFINITY;
}

function readMeasuredAt(sample?: ServerLatencySample): number {
  return sample?.measuredAt ?? 0;
}

function normalize(value?: string): string {
  return value?.trim() || "";
}

function normalizeDirectory(value?: string): string {
  const trimmed = normalize(value);
  if (!trimmed || trimmed === "/") {
    return trimmed;
  }

  const normalized = trimmed.replace(/\/+$/, "");
  return normalized || "/";
}

function basename(value: string): string {
  if (!value || value === "/") {
    return value || "/";
  }

  const parts = value.split("/").filter(Boolean);
  return parts[parts.length - 1] || value;
}
