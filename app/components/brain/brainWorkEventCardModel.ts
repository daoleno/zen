import type { BrainWorkResultEvent } from "./brainWorkEvent";
import { brainWorkEventSummary } from "./brainWorkEventPresentation";
import { BRAIN_WORK_CARD_MAX_FACTS } from "./brainWorkEventCardLayout";

export type BrainWorkEventCardModel = {
  density: "minimal" | "rich";
  summary?: string;
  facts: string[];
};

const MAX_FACT_LENGTH = 96;
const RESERVED_DETAIL_KEYS = new Set([
  "attention",
  "criteria_met",
  "event_kind",
  "next_action",
  "phase",
  "summary",
  "wait_for",
]);

export function brainWorkEventCardModel(
  event: BrainWorkResultEvent,
): BrainWorkEventCardModel {
  const details = detailFacts(event.details_json);
  const phase = meaningfulPhase(event.phase);
  const attention = meaningfulAttention(event.attention);
  const eventKind = meaningfulEventKind(event.event_kind);
  const hasContext = Boolean(
    phase || attention || eventKind || details.length > 0,
  );
  if (!hasContext) {
    return { density: "minimal", facts: [] };
  }

  const summary = clean(brainWorkEventSummary(event));
  const facts: string[] = [];
  addFact(facts, waitFact(event.wait_for), summary);
  details.forEach((fact) => addFact(facts, fact, summary));
  addFact(facts, nextActionFact(event.next_action), summary);

  return {
    density: "rich",
    summary: summary || phase || eventKind || attention,
    facts: facts.slice(0, BRAIN_WORK_CARD_MAX_FACTS),
  };
}

function detailFacts(raw: string | undefined): string[] {
  if (!raw) {
    return [];
  }
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return [];
  }
  const facts: string[] = [];
  for (const [key, candidate] of Object.entries(value)) {
    const normalizedKey = key.trim().toLowerCase();
    if (!normalizedKey || RESERVED_DETAIL_KEYS.has(normalizedKey)) {
      continue;
    }
    const displayValue = scalarValue(candidate);
    if (!displayValue) {
      continue;
    }
    facts.push(`${detailLabel(normalizedKey)}: ${displayValue}`);
    if (facts.length === BRAIN_WORK_CARD_MAX_FACTS) {
      break;
    }
  }
  return facts;
}

function scalarValue(value: unknown): string | undefined {
  if (typeof value === "string") {
    return truncate(clean(value));
  }
  if (typeof value === "number" && Number.isFinite(value)) {
    return String(value);
  }
  if (typeof value === "boolean") {
    return value ? "Yes" : "No";
  }
  if (
    Array.isArray(value) &&
    value.length > 0 &&
    value.length <= 3 &&
    value.every((item) => typeof item === "string" || typeof item === "number")
  ) {
    return truncate(value.map(String).join(", "));
  }
  return undefined;
}

function detailLabel(key: string): string {
  return key
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part, index) => {
      const upper = part.toUpperCase();
      if (upper === "CI" || upper === "ID" || upper === "URL") {
        return upper;
      }
      return index === 0
        ? part.charAt(0).toUpperCase() + part.slice(1)
        : part;
    })
    .join(" ");
}

function meaningfulPhase(value: string | undefined): string | undefined {
  const phase = clean(value);
  if (
    !phase ||
    phase === "working" ||
    phase === "starting" ||
    phase === "reporting"
  ) {
    return undefined;
  }
  return sentenceCase(phase);
}

function meaningfulAttention(value: string | undefined): string | undefined {
  switch (clean(value)) {
    case "user_input":
      return "User input required";
    case "blocked":
      return "Blocked";
    case "failed":
      return "Failed";
    case "stale":
      return "Progress overdue";
    default:
      return undefined;
  }
}

function meaningfulEventKind(value: string | undefined): string | undefined {
  const kind = clean(value);
  if (!kind || kind === "progress" || kind === "done") {
    return undefined;
  }
  return sentenceCase(kind.replace(/_/g, " "));
}

function waitFact(value: string | undefined): string | undefined {
  const waitFor = clean(value);
  if (!waitFor) {
    return undefined;
  }
  return /^waiting\b/i.test(waitFor) ? waitFor : `Waiting for ${waitFor}`;
}

function nextActionFact(value: string | undefined): string | undefined {
  const nextAction = clean(value);
  return nextAction ? `Next: ${nextAction}` : undefined;
}

function addFact(
  facts: string[],
  candidate: string | undefined,
  summary: string | undefined,
) {
  const fact = truncate(clean(candidate));
  if (
    !fact ||
    equivalent(fact, summary) ||
    facts.some((item) => equivalent(item, fact))
  ) {
    return;
  }
  facts.push(fact);
}

function equivalent(left: string | undefined, right: string | undefined) {
  if (!left || !right) {
    return false;
  }
  const normalize = (value: string) =>
    value.toLowerCase().replace(/[^a-z0-9]+/g, " ").trim();
  const a = normalize(left);
  const b = normalize(right);
  return a === b || a.includes(b) || b.includes(a);
}

function clean(value: string | undefined): string | undefined {
  const normalized = value?.replace(/\s+/g, " ").trim();
  return normalized || undefined;
}

function truncate(value: string | undefined): string | undefined {
  if (!value || value.length <= MAX_FACT_LENGTH) {
    return value;
  }
  return `${value.slice(0, MAX_FACT_LENGTH - 1).trimEnd()}...`;
}

function sentenceCase(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}
