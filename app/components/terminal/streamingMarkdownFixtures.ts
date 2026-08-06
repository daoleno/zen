/**
 * Deterministic streaming Markdown body fixtures for coalescing tests/benchmarks.
 * Bodies are precomputed by callers; timed regions must not construct them.
 */

export type StreamingMarkdownFixtureKind =
  | "plainText"
  | "markdownListTable"
  | "unfinishedFence"
  | "largeCode";

const PLAIN_SEED =
  "Streaming plain text revision grows steadily without markup structure. ";

const LIST_TABLE_SEED = `## Status

- item alpha
- item beta
- item gamma

| Col A | Col B | Col C |
| --- | --- | --- |
| a1 | b1 | c1 |
| a2 | b2 | c2 |

`;

const UNFINISHED_FENCE_PREFIX = `Notes before fence

\`\`\`ts
`;

export function streamingMarkdownFixtureBody(
  kind: StreamingMarkdownFixtureKind,
  revision: number,
): string {
  const safeRevision = Math.max(0, revision);
  switch (kind) {
    case "plainText":
      return PLAIN_SEED.repeat(2 + (safeRevision % 5) + Math.floor(safeRevision / 3));
    case "markdownListTable":
      return (
        LIST_TABLE_SEED +
        `- live ${safeRevision}\n\n` +
        `Trailing paragraph revision ${safeRevision}. `.repeat(2 + (safeRevision % 4))
      );
    case "unfinishedFence":
      return (
        UNFINISHED_FENCE_PREFIX +
        `const revision = ${safeRevision};\n`.repeat(3 + (safeRevision % 6)) +
        `// still open fence at revision ${safeRevision}\n`
      );
    case "largeCode": {
      const lineCount = 40 + safeRevision * 3;
      let body = "```ts\n";
      for (let index = 0; index < lineCount; index += 1) {
        body += `export const line_${index + safeRevision} = ${index + safeRevision};\n`;
      }
      // Most streaming revisions leave the fence open; occasional close keeps
      // parse diversity without claiming a second presentation model.
      if (safeRevision > 0 && safeRevision % 17 === 0) {
        body += "```\n";
      }
      return body;
    }
    default: {
      const _exhaustive: never = kind;
      return _exhaustive;
    }
  }
}

export function buildStreamingMarkdownRevisions(
  kind: StreamingMarkdownFixtureKind,
  count: number,
): string[] {
  const revisions: string[] = [];
  for (let revision = 0; revision < count; revision += 1) {
    revisions.push(streamingMarkdownFixtureBody(kind, revision));
  }
  return revisions;
}

export const STREAMING_MARKDOWN_FIXTURE_KINDS: StreamingMarkdownFixtureKind[] = [
  "plainText",
  "markdownListTable",
  "unfinishedFence",
  "largeCode",
];
