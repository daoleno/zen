export type HeartbeatWakeEvent = {
  reason?: string;
  agentId?: string;
  agentName?: string;
  status?: string;
  oldState?: string;
  newState?: string;
  workspace?: string;
  summary?: string;
  trailingPrompt?: string;
};

// Historical transcript projection only. Work/Event is the runtime scheduler;
// current daemon paths never emit this legacy message shape.
const HEARTBEAT_HEADER_RE = /^Heartbeat wake:\s*$/i;
const HEARTBEAT_FIELD_RE = /^([A-Za-z_][A-Za-z0-9_-]*):\s*(.*)$/;

export function parseHeartbeatWakeMessage(value: string): HeartbeatWakeEvent | null {
  const normalized = value.replace(/\r\n/g, "\n").replace(/\r/g, "\n").trim();
  if (!normalized) {
    return null;
  }

  const lines = normalized.split("\n");
  if (!HEARTBEAT_HEADER_RE.test(lines[0].trim())) {
    return null;
  }

  const fields = new Map<string, string>();
  let currentKey: string | null = null;
  let index = 1;
  for (; index < lines.length; index += 1) {
    const rawLine = lines[index];
    if (!rawLine.trim()) {
      index += 1;
      break;
    }

    const fieldMatch = HEARTBEAT_FIELD_RE.exec(rawLine.trimEnd());
    if (fieldMatch) {
      currentKey = normalizeHeartbeatKey(fieldMatch[1]);
      fields.set(currentKey, fieldMatch[2].trim());
      continue;
    }

    if (currentKey && /^\s+/.test(rawLine)) {
      const previous = fields.get(currentKey) || "";
      fields.set(currentKey, [previous, rawLine.trim()].filter(Boolean).join(" "));
      continue;
    }

    break;
  }

  if (fields.size === 0) {
    return null;
  }

  const event: HeartbeatWakeEvent = {
    reason: fieldValue(fields, "reason"),
    agentId: fieldValue(fields, "agent_id"),
    agentName: fieldValue(fields, "agent_name"),
    status: fieldValue(fields, "status"),
    oldState: fieldValue(fields, "old_state"),
    newState: fieldValue(fields, "new_state"),
    workspace: fieldValue(fields, "workspace"),
    summary: fieldValue(fields, "summary"),
    trailingPrompt: lines.slice(index).join("\n").trim() || undefined,
  };

  return hasHeartbeatSignal(event) ? event : null;
}

export function formatHeartbeatReason(value?: string): string {
  return formatHeartbeatValue(value || "wake");
}

export function formatHeartbeatValue(value?: string): string {
  return humanizeHeartbeatToken(value || "");
}

export function formatHeartbeatStateChange(event: HeartbeatWakeEvent): string {
  if (event.oldState && event.newState) {
    return `${formatHeartbeatValue(event.oldState)} -> ${formatHeartbeatValue(event.newState)}`;
  }
  if (event.status) {
    return formatHeartbeatValue(event.status);
  }
  return "State changed";
}

function fieldValue(fields: Map<string, string>, key: string): string | undefined {
  return fields.get(key)?.trim() || undefined;
}

function normalizeHeartbeatKey(value: string): string {
  return value.trim().toLowerCase().replace(/-/g, "_");
}

function hasHeartbeatSignal(event: HeartbeatWakeEvent): boolean {
  return Boolean(
    event.reason ||
      event.agentId ||
      event.agentName ||
      event.status ||
      event.oldState ||
      event.newState ||
      event.workspace ||
      event.summary,
  );
}

function humanizeHeartbeatToken(value: string): string {
  const normalized = value.trim();
  if (!normalized) {
    return "";
  }
  return normalized
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}
