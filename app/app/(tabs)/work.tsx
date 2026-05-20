import React, { useMemo } from "react";
import { ScrollView, StyleSheet, Text, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { MarkdownView } from "../../components/work/MarkdownView";
import {
  Colors,
  Typography,
  useAppColors,
} from "../../constants/tokens";
import { useWork, type WorkItem } from "../../store/work";

type BrainEntry = {
  key: string;
  date: string;
  project: string;
  title: string;
  body: string;
  sections: BrainSections;
  score: number;
  updated: number;
};

type BrainSections = {
  outcome: string;
  readout: string;
  signals: string[];
  friction: string;
  cause: string;
  insight: string;
  next: string;
};

export default function BrainScreen() {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const { state } = useWork();

  const brainItems = useMemo(
    () => Object.values(state.byKey).filter(isBrainItem).sort(sortBrainItems),
    [state.byKey],
  );
  const brainMarkdown = useMemo(
    () => buildBrainMarkdown(brainItems),
    [brainItems],
  );
  const markdownValue =
    brainMarkdown ||
    "## Brain\n\n暂无 agent 会话读数。";

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <View style={styles.header}>
        <Text style={styles.title}>Brain</Text>
      </View>
      <ScrollView
        style={styles.content}
        contentContainerStyle={styles.contentContainer}
        showsVerticalScrollIndicator={false}
      >
        <MarkdownView value={markdownValue} />
      </ScrollView>
    </SafeAreaView>
  );
}

function buildBrainMarkdown(items: WorkItem[]): string {
  const entries = items
    .flatMap(extractProjectEntries)
    .filter(hasEntryReadout)
    .sort(sortEntries);
  if (entries.length === 0) {
    return "";
  }

  const lines: string[] = [];
  for (const group of groupEntriesByDate(entries)) {
    if (lines.length > 0) {
      lines.push("");
    }
    lines.push(`## ${group.date}`, "");

    const summary = dailyLines(group.entries, (entry) =>
      firstNonEmpty(entry.sections.outcome, entry.sections.readout),
    ).slice(0, 4);
    if (summary.length > 0) {
      lines.push("### Summary", "");
      lines.push(...summary);
      lines.push("");
    }

    const insights = dailyLines(
      group.entries,
      (entry) => entry.sections.insight,
    ).slice(0, 4);
    if (insights.length > 0) {
      lines.push("### Insights", "");
      lines.push(...insights);
      lines.push("");
    }

    const friction = dailyLines(group.entries, dailyFriction).slice(0, 4);
    if (friction.length > 0) {
      lines.push("### Friction", "");
      lines.push(...friction);
      lines.push("");
    }

    const next = dailyLines(group.entries, (entry) => entry.sections.next).slice(
      0,
      3,
    );
    if (next.length > 0) {
      lines.push("### Next", "");
      lines.push(...next);
      lines.push("");
    }

    const evidence = dailyLines(group.entries, dailyEvidence).slice(0, 8);
    if (evidence.length > 0) {
      lines.push("### Evidence", "");
      lines.push(...evidence);
    }
  }

  return `${lines.join("\n").trim()}\n`;
}

function dailyLines(
  entries: BrainEntry[],
  pick: (entry: BrainEntry) => string,
): string[] {
  const out: string[] = [];
  const seen = new Set<string>();

  for (const entry of entries) {
    const text = cleanDailyText(pick(entry));
    if (!text) {
      continue;
    }
    const key = text.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    out.push(`- **${entry.project}:** ${truncateText(text, 220)}`);
  }

  return out;
}

function dailyFriction(entry: BrainEntry): string {
  const friction = cleanDailyText(entry.sections.friction);
  const cause = cleanDailyText(entry.sections.cause);
  if (friction && cause) {
    return `${friction}; cause: ${cause}`;
  }
  return friction || cause;
}

function dailyEvidence(entry: BrainEntry): string {
  const parts = [
    firstNonEmpty(entry.sections.outcome, entry.sections.readout),
    entry.sections.signals[0],
  ]
    .map(cleanDailyText)
    .filter(Boolean);
  return parts.join(" ");
}

function groupEntriesByDate(entries: BrainEntry[]) {
  const groups: Array<{ date: string; entries: BrainEntry[] }> = [];
  for (const entry of entries) {
    const last = groups[groups.length - 1];
    if (last?.date === entry.date) {
      last.entries.push(entry);
    } else {
      groups.push({ date: entry.date, entries: [entry] });
    }
  }
  return groups;
}

function extractProjectEntries(item: WorkItem): BrainEntry[] {
  const project = projectTitle(item);
  const entries = parseSessionEntries(item, project);
  if (entries.length > 0) {
    return entries;
  }

  return [fallbackEntry(item, project)];
}

function parseSessionEntries(item: WorkItem, project: string): BrainEntry[] {
  const body = item.body || "";
  const pattern =
    /<!--\s*zen:session:start\s+([^\s]+)([^>]*)-->([\s\S]*?)<!--\s*zen:session:end\s+\1\s*-->/g;
  const entries: BrainEntry[] = [];
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(body)) !== null) {
    const key = match[1];
    const meta = parseSessionMeta(match[2]);
    const rawContent = stripComments(match[3]).trim();
    const titleMatch = /^###\s+(.+)$/m.exec(rawContent);
    const title = titleMatch?.[1]?.trim() || project;
    const content = cleanEntryBody(
      rawContent.replace(/^\s*###\s+.+(?:\n|$)/, ""),
    );
    const sections = splitEntryBody(content);

    entries.push({
      key: `${item.key}:${key}`,
      date: meta.date || dateFromItem(item),
      project,
      title,
      body: content,
      sections,
      score: parseNumber(meta.score),
      updated: timestampMillis(meta.updated) || timestampMillis(item.mtime),
    });
  }

  return entries;
}

function fallbackEntry(item: WorkItem, project: string): BrainEntry {
  const body = bodyFromFrontmatter(item);
  const sections = splitEntryBody(body);
  return {
    key: item.key,
    date: dateFromItem(item),
    project,
    title: project,
    body,
    sections,
    score: body ? 1 : 0,
    updated: timestampMillis(item.frontmatter.ai_updated || item.mtime),
  };
}

function bodyFromFrontmatter(item: WorkItem): string {
  const lines: string[] = [];
  const outcome = cleanInline(item.frontmatter.outcome);
  if (outcome && !isPlaceholderText(outcome)) {
    lines.push("#### Outcome", "", outcome);
  }

  const summary = cleanInline(item.frontmatter.summary);
  if (summary && !isPlaceholderText(summary)) {
    lines.push("", "#### Read", "", summary);
  }

  const progress = Array.isArray(item.frontmatter.progress)
    ? item.frontmatter.progress
        .map(cleanInline)
        .filter((entry) => entry && !isPlaceholderText(entry))
    : [];
  if (progress.length > 0) {
    lines.push("", "#### Signals", "", ...progress.map((entry) => `- ${entry}`));
  }

  const friction = cleanInline(item.frontmatter.friction);
  if (friction && !isPlaceholderText(friction)) {
    lines.push("", "#### Friction", "", friction);
  }

  const cause = cleanInline(item.frontmatter.cause);
  if (cause && !isPlaceholderText(cause)) {
    lines.push("", "#### Cause", "", cause);
  }

  const insight = cleanInline(item.frontmatter.insight);
  if (insight && !isPlaceholderText(insight)) {
    lines.push("", "#### Insight", "", insight);
  }

  const next = cleanInline(item.frontmatter.next);
  if (next && !isPlaceholderText(next)) {
    lines.push("", "#### Next", "", next);
  }

  return cleanEntryBody(lines.join("\n"));
}

function splitEntryBody(value: string): {
  outcome: string;
  readout: string;
  signals: string[];
  friction: string;
  cause: string;
  insight: string;
  next: string;
} {
  const outcome: string[] = [];
  const readout: string[] = [];
  const signals: string[] = [];
  const friction: string[] = [];
  const cause: string[] = [];
  const insight: string[] = [];
  const next: string[] = [];
  let section:
    | "outcome"
    | "readout"
    | "signals"
    | "friction"
    | "cause"
    | "insight"
    | "next" = "readout";

  for (const rawLine of cleanEntryBody(value).split("\n")) {
    const line = rawLine.trim();
    if (!line) {
      continue;
    }

    const heading = /^#{1,6}\s+(.+)$/.exec(line)?.[1]?.trim().toLowerCase();
    if (heading) {
      if (
        heading === "outcome" ||
        heading === "result" ||
        heading === "结果"
      ) {
        section = "outcome";
      } else if (
        heading === "read" ||
        heading === "readout" ||
        heading === "summary" ||
        heading === "brief" ||
        heading === "takeaway" ||
        heading === "总结"
      ) {
        section = "readout";
      } else if (
        heading === "signals" ||
        heading === "useful signals" ||
        heading === "关键结论"
      ) {
        section = "signals";
      } else if (
        heading === "friction" ||
        heading === "drag" ||
        heading === "问题"
      ) {
        section = "friction";
      } else if (
        heading === "cause" ||
        heading === "diagnosis" ||
        heading === "原因"
      ) {
        section = "cause";
      } else if (
        heading === "insight" ||
        heading === "lesson" ||
        heading === "洞察"
      ) {
        section = "insight";
      } else if (
        heading === "next" ||
        heading === "next useful move" ||
        heading === "下一步"
      ) {
        section = "next";
      }
      continue;
    }

    const bullet = /^[-*]\s+(.+)$/.exec(line)?.[1]?.trim();
    const text = cleanInline(bullet || line);
    if (!text || isPlaceholderText(text)) {
      continue;
    }
    if (section === "signals") {
      signals.push(text);
    } else if (section === "outcome") {
      outcome.push(text);
    } else if (section === "friction") {
      friction.push(text);
    } else if (section === "cause") {
      cause.push(text);
    } else if (section === "insight") {
      insight.push(text);
    } else if (section === "next") {
      next.push(text);
    } else {
      readout.push(text);
    }
  }

  return {
    outcome: compactText(outcome.join(" ")),
    readout: compactText(readout.join(" ")),
    signals: signals.map(compactText).filter(Boolean).slice(0, 3),
    friction: compactText(friction.join(" ")),
    cause: compactText(cause.join(" ")),
    insight: compactText(insight.join(" ")),
    next: compactText(next.join(" ")),
  };
}

function cleanEntryBody(value: string): string {
  const cleaned = stripComments(value)
    .replace(/^####\s+Useful Signals\s*$/gim, "#### Signals")
    .replace(/^####\s+关键结论\s*$/gim, "#### Signals")
    .replace(/^####\s+Next Useful Move\s*$/gim, "#### Next")
    .replace(/^####\s+下一步\s*$/gim, "#### Next")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
  return isPlaceholderText(cleaned) ? "" : cleaned;
}

function stripComments(value: string): string {
  return value.replace(/<!--[\s\S]*?-->/g, "");
}

function parseSessionMeta(value: string): Record<string, string> {
  const meta: Record<string, string> = {};
  for (const field of value.trim().split(/\s+/)) {
    const [key, rawValue] = field.split("=");
    if (!key || rawValue === undefined) {
      continue;
    }
    meta[key.trim()] = rawValue.trim();
  }
  return meta;
}

function entryTitle(entry: BrainEntry): string {
  if (!entry.title || entry.title === entry.project) {
    return entry.project;
  }
  return `${entry.project} - ${entry.title}`;
}

function projectTitle(item: WorkItem): string {
  return (
    cleanInline(item.frontmatter.title) ||
    cleanInline(item.project) ||
    cleanInline(item.title) ||
    "workspace"
  );
}

function hasEntryReadout(entry: BrainEntry): boolean {
  const sections = entry.sections;
  return Boolean(
    sections.outcome ||
      sections.readout ||
      sections.signals.length > 0 ||
      sections.friction ||
      sections.cause ||
      sections.insight ||
      sections.next,
  );
}

function cleanInline(value: unknown): string {
  return typeof value === "string" ? value.replace(/\s+/g, " ").trim() : "";
}

function cleanDailyText(value: unknown): string {
  return cleanInline(value)
    .replace(/\b[Tt]his session\b/g, "this work")
    .replace(/\b[Tt]he session\b/g, "the work")
    .replace(/\b[Tt]his agent round\b/g, "this work")
    .replace(/\b[Tt]his round\b/g, "this work")
    .replace(/这轮\s*Agent/g, "当天工作")
    .replace(/本轮\s*Agent/g, "当天工作")
    .replace(/这轮\s*agent/g, "当天工作")
    .replace(/本轮\s*agent/g, "当天工作")
    .trim();
}

function compactText(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

function firstNonEmpty(...values: Array<string | undefined>): string {
  for (const value of values) {
    const text = cleanDailyText(value);
    if (text) {
      return text;
    }
  }
  return "";
}

function truncateText(value: string, max: number): string {
  if (value.length <= max) {
    return value;
  }
  if (max <= 3) {
    return value.slice(0, max);
  }
  return `${value.slice(0, max - 3).trim()}...`;
}

function isPlaceholderText(value: string): boolean {
  const normalized = value.replace(/\s+/g, " ").trim().toLowerCase();
  return (
    normalized === "" ||
    normalized === "ai digest pending." ||
    normalized === "ai digest pending" ||
    normalized === "ai digest unavailable." ||
    normalized.includes("ai digest unavailable") ||
    normalized.includes("analyzing session") ||
    normalized.includes("no readout")
  );
}

function dateFromItem(item: WorkItem): string {
  const millis =
    timestampMillis(item.frontmatter.ai_updated) ||
    timestampMillis(item.mtime) ||
    timestampMillis(item.frontmatter.created);
  if (!millis) {
    return "Undated";
  }
  return localDateKey(new Date(millis));
}

function localDateKey(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function parseNumber(value?: string): number {
  const parsed = Number.parseInt(value || "", 10);
  return Number.isFinite(parsed) ? parsed : 0;
}

function timestampMillis(value?: string | number | null) {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value > 10_000_000_000 ? value : value * 1000;
  }
  if (typeof value === "string" && value.trim()) {
    const parsed = Date.parse(value);
    return Number.isNaN(parsed) ? 0 : parsed;
  }
  return 0;
}

function sortEntries(left: BrainEntry, right: BrainEntry): number {
  if (left.date !== right.date) {
    return right.date.localeCompare(left.date);
  }
  if (left.score !== right.score) {
    return right.score - left.score;
  }
  if (left.updated !== right.updated) {
    return right.updated - left.updated;
  }
  return entryTitle(left).localeCompare(entryTitle(right));
}

function isBrainItem(item: WorkItem): boolean {
  return (
    item.frontmatter.kind === "brain_log" &&
    isNativeAgentSource(item.frontmatter.agent_source)
  );
}

function isNativeAgentSource(source: unknown): boolean {
  return source === "codex" || source === "claude";
}

function sortBrainItems(left: WorkItem, right: WorkItem): number {
  return projectTitle(left).localeCompare(projectTitle(right));
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    screen: {
      flex: 1,
      backgroundColor: colors.bgPrimary,
    },
    header: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      paddingHorizontal: 16,
      paddingTop: 8,
      paddingBottom: 10,
    },
    title: {
      flexShrink: 0,
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 22,
      lineHeight: 28,
      opacity: 0.92,
    },
    content: {
      flex: 1,
    },
    contentContainer: {
      paddingBottom: 20,
    },
  });
}
