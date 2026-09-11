import {
  mermaidComplexityExceeded,
  measureMermaidComplexity,
} from "./mermaidLimits";
import {
  mermaidSourceLooksUnsafe,
  prepareTrustedMermaidSource,
} from "./mermaidSecurity";

export type MermaidDiagramKind = "flowchart";

export type MermaidPrepareFailure =
  | "incomplete"
  | "unsupported"
  | "oversized"
  | "unsafe"
  | "empty";

export type MermaidPrepareResult =
  | {
      ok: true;
      source: string;
      kind: MermaidDiagramKind;
      complexity: ReturnType<typeof measureMermaidComplexity>;
    }
  | {
      ok: false;
      reason: MermaidPrepareFailure;
    };

const FLOWCHART_HEADER =
  /^(?:flowchart|graph)\s+(?:TD|TB|BT|RL|LR)(?:\s|$)/i;
const UNSUPPORTED_HEADER =
  /^(?:sequenceDiagram|classDiagram|stateDiagram(?:-v2)?|erDiagram|journey|gantt|pie|gitGraph|mindmap|timeline|quadrantChart|requirementDiagram|C4Context|C4Container|C4Component|C4Dynamic|sankey-beta|xychart-beta|kanban|block-beta|packet-beta|architecture-beta|zenuml|radar-beta)\b/i;

export function detectMermaidDiagramKind(
  source: string,
): MermaidDiagramKind | "unsupported" | "empty" {
  const header = firstDiagramHeader(source);
  if (!header) {
    return "empty";
  }
  if (FLOWCHART_HEADER.test(header)) {
    return "flowchart";
  }
  if (UNSUPPORTED_HEADER.test(header) || !/^(?:flowchart|graph)\b/i.test(header)) {
    return "unsupported";
  }
  return "flowchart";
}

export function prepareMermaidDiagram(
  raw: string,
  options: { streaming?: boolean; closed?: boolean } = {},
): MermaidPrepareResult {
  if (options.streaming || options.closed === false) {
    return { ok: false, reason: "incomplete" };
  }
  const source = prepareTrustedMermaidSource(raw);
  if (!source) {
    return { ok: false, reason: "empty" };
  }
  if (mermaidSourceLooksUnsafe(source)) {
    return { ok: false, reason: "unsafe" };
  }
  const kind = detectMermaidDiagramKind(source);
  if (kind === "empty") {
    return { ok: false, reason: "empty" };
  }
  if (kind === "unsupported") {
    return { ok: false, reason: "unsupported" };
  }
  if (mermaidComplexityExceeded(source)) {
    return { ok: false, reason: "oversized" };
  }
  return {
    ok: true,
    source,
    kind,
    complexity: measureMermaidComplexity(source),
  };
}

export function mermaidFailureLabel(reason: MermaidPrepareFailure) {
  switch (reason) {
    case "incomplete":
      return "Diagram";
    case "unsupported":
      return "Unsupported diagram";
    case "oversized":
      return "Diagram too large";
    case "unsafe":
      return "Diagram blocked";
    case "empty":
    default:
      return "Diagram";
  }
}

function firstDiagramHeader(source: string) {
  for (const line of source.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("%%") || trimmed.startsWith("#")) {
      continue;
    }
    return trimmed;
  }
  return "";
}
