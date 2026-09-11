export const MERMAID_MAX_SOURCE_BYTES = 32_768;
export const MERMAID_MAX_NODES = 80;
export const MERMAID_MAX_EDGES = 120;
export const MERMAID_MAX_SUBGRAPHS = 16;
export const MERMAID_MAX_RENDER_MS = 2_500;
export const MERMAID_MAX_SVG_BYTES = 400_000;
export const MERMAID_MAX_POST_MESSAGE_BYTES = 512_000;
export const MERMAID_CACHE_LIMIT = 24;
export const MERMAID_MAX_INFLIGHT_RENDERS = 1;
export const MERMAID_MAX_QUEUED_RENDERS = 8;
export const MERMAID_MAX_INLINE_PREVIEW_WEBVIEWS = 3;
export const MERMAID_INLINE_MAX_HEIGHT = 280;
export const MERMAID_MESSAGE_VERSION = 1;

const NODE_PATTERN =
  /(?:^|[\s;])(?:subgraph\s+)?[A-Za-z_][\w-]*(?:\s*(?:\[[^\]]*\]|\([^)]*\)|\{\{?[^}]*\}?\}|>[^<]*<|\(\([^)]*\)\)|\(\[[^\]]*\]\)|\[\[[^\]]*\]\]|\[\/[^\]]*\/\]|\[\([^\]]*\)\]))/gm;
const EDGE_PATTERN = /-{1,3}\.?-*>|<-->|<-+>|\.{2,3}>|={2,}>|---|-->|\.->/g;
const SUBGRAPH_PATTERN = /^\s*subgraph\b/gm;

export type MermaidComplexity = {
  nodes: number;
  edges: number;
  subgraphs: number;
  bytes: number;
};

export function measureMermaidComplexity(source: string): MermaidComplexity {
  const bytes = byteLength(source);
  const nodes = uniqueCount(source.match(NODE_PATTERN));
  const edges = source.match(EDGE_PATTERN)?.length ?? 0;
  const subgraphs = source.match(SUBGRAPH_PATTERN)?.length ?? 0;
  return { nodes, edges, subgraphs, bytes };
}

export function mermaidComplexityExceeded(source: string): boolean {
  const complexity = measureMermaidComplexity(source);
  return (
    complexity.bytes > MERMAID_MAX_SOURCE_BYTES ||
    complexity.nodes > MERMAID_MAX_NODES ||
    complexity.edges > MERMAID_MAX_EDGES ||
    complexity.subgraphs > MERMAID_MAX_SUBGRAPHS
  );
}

export function byteLength(value: string) {
  return new TextEncoder().encode(value).length;
}

function uniqueCount(matches: RegExpMatchArray | null) {
  if (!matches) {
    return 0;
  }
  return new Set(matches.map((match) => match.trim())).size;
}
