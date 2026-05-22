import type { CodexConversationEvent } from "../../services/codexConversation";
import type { ChatCommandEvent } from "./CodexChatSession";
import {
  type PatchFileSummary,
  type PatchOperation,
  type ZenActivityTimelineItem,
} from "./CodexTimelineActivity";
import type { DisplayAttachment } from "./CodexTimelineMessage";
import type { ZenTimelineItem } from "./CodexTimelineItemView";
import { slashCommandIcon } from "./codexSlashCommandPresentation";

const ATTACHMENT_TAG_RE = /<zen_attachments>\s*([\s\S]*?)\s*<\/zen_attachments>/i;
const COMMAND_OUTPUT_PREVIEW_LINES = 7;
const COMMAND_OUTPUT_PREVIEW_CHARS = 1200;
const TOOL_PAYLOAD_PREVIEW_LINES = 6;
const TOOL_PAYLOAD_PREVIEW_CHARS = 1000;
const FULL_OUTPUT_HINT = "Open Terminal for full output.";

type TimelineIconName = ZenActivityTimelineItem["icon"];

type ToolPresentation = {
  subtitle?: string;
  icon: TimelineIconName;
  localImagePath?: string;
};

type CommandKind =
  | "read"
  | "list"
  | "search"
  | "test"
  | "check"
  | "git"
  | "install"
  | "run";

type CommandPresentation = {
  kind: CommandKind;
  target?: string;
  query?: string;
  detail?: string;
  icon: TimelineIconName;
  runningTitle: string;
  doneTitle: string;
  failedTitle: string;
  groupable: boolean;
  explorationLabel?: string;
};

type OutputPreview = {
  text: string;
  truncated: boolean;
};

type OutputPreviewOptions = {
  maxLines: number;
  maxChars: number;
};

type ExplorationEntry = {
  event: CodexConversationEvent;
  presentation: CommandPresentation;
  running: boolean;
  failed: boolean;
  output: OutputPreview;
};

type PatchSummary = {
  title: string;
  files: PatchFileSummary[];
  totalAdded: number;
  totalRemoved: number;
};

export function latestAssistantMessageBody(events: CodexConversationEvent[]) {
  for (let index = events.length - 1; index >= 0; index--) {
    const event = events[index];
    if (event.kind === "assistant_message" && event.body?.trim()) {
      return event.body.trim();
    }
  }
  return "";
}

export function buildZenTimeline(events: CodexConversationEvent[]): ZenTimelineItem[] {
  const items: ZenTimelineItem[] = [];
  let explorationEntries: ExplorationEntry[] = [];

  const flushExploration = () => {
    if (explorationEntries.length === 0) {
      return;
    }
    items.push(explorationActivityFromEntries(explorationEntries));
    explorationEntries = [];
  };

  for (const event of [...events].sort((left, right) => left.seq - right.seq)) {
    if (event.kind === "user_message" || event.kind === "assistant_message") {
      flushExploration();
      const extracted = extractDisplayMessage(event.body || "");
      if (!extracted.body && extracted.attachments.length === 0) {
        continue;
      }
      items.push({
        type: "message",
        id: event.id || `${event.kind}:${event.seq}`,
        role: event.kind === "user_message" ? "user" : "assistant",
        timestamp: event.timestamp,
        body: extracted.body,
        attachments: extracted.attachments,
      });
      continue;
    }

    if (event.kind === "plan") {
      flushExploration();
      items.push({
        type: "plan",
        id: event.id || `plan:${event.seq}`,
        timestamp: event.timestamp,
        explanation: event.explanation || event.body,
        steps: event.plan ?? [],
      });
      continue;
    }

    if (event.kind === "command") {
      const entry = explorationEntryFromEvent(event);
      if (entry) {
        explorationEntries.push(entry);
        continue;
      }
      flushExploration();
    } else {
      flushExploration();
    }

    const activity = activityFromEvent(event);
    if (activity) {
      items.push(activity);
    }
  }
  flushExploration();
  return items;
}

export function mergeChatCommandEventsIntoTimeline(
  timelineItems: ZenTimelineItem[],
  commandEvents: ChatCommandEvent[],
): ZenTimelineItem[] {
  if (commandEvents.length === 0) {
    return timelineItems;
  }
  return [
    ...timelineItems,
    ...commandEvents.map((event) => ({
      type: "activity" as const,
      id: event.id,
      timestamp: event.createdAt,
      title: event.title,
      tone: event.tone,
      icon:
        event.tone === "failed"
          ? "alert-circle-outline"
          : event.tone === "success"
            ? slashCommandIcon(event.command.name)
            : "terminal-outline",
      detail: event.detail,
      body: event.body,
    })),
  ];
}

function activityFromEvent(event: CodexConversationEvent): ZenTimelineItem | null {
  switch (event.kind) {
    case "command": {
      const presentation = commandPresentation(event.command || "");
      const failed = isCommandFailed(event, presentation);
      const running = event.status === "running";
      const command = event.command || "";
      const output = formatOutputPreview(event.body || "", {
        maxLines: COMMAND_OUTPUT_PREVIEW_LINES,
        maxChars: COMMAND_OUTPUT_PREVIEW_CHARS,
      });
      return {
        type: "activity",
        id: event.id || `command:${event.seq}`,
        timestamp: event.timestamp,
        title: commandActivityTitle(command, running, failed, presentation),
        tone: running ? "running" : failed ? "failed" : "success",
        icon: running ? "time-outline" : failed ? "alert-circle-outline" : presentation.icon,
        detail: presentation.detail || commandSummary(command),
        body: output.text || (!running && !failed ? "(no output)" : undefined),
      };
    }
    case "patch": {
      const summary = patchSummaryFromEvent(event);
      return {
        type: "activity",
        id: event.id || `patch:${event.seq}`,
        timestamp: event.timestamp,
        title: summary.title,
        tone: "success",
        icon: "git-compare-outline",
        fileSummaries: summary.files,
        files: summary.files.map((file) => file.path),
        body: summary.files.length > 0 ? undefined : event.body,
      };
    }
    case "tool": {
      const name = event.tool_name || event.title || "tool";
      if (isLowSignalToolEvent(name, event.input || "")) {
        return null;
      }
      const failed = event.status === "failed" || (event.exit_code ?? 0) !== 0;
      const running = event.status === "running";
      const presentation = toolPresentation(event);
      const previewPath = presentation.localImagePath || imagePathFromTool(event);
      const result = formatOutputPreview(event.output || event.body || "", {
        maxLines: TOOL_PAYLOAD_PREVIEW_LINES,
        maxChars: TOOL_PAYLOAD_PREVIEW_CHARS,
      });
      const heading = toolActivityHeading(event, running);
      return {
        type: "activity",
        id: event.id || `tool:${event.seq}`,
        timestamp: event.timestamp,
        title: heading.title,
        tone: running ? "running" : failed ? "failed" : "success",
        icon: presentation.icon,
        detail: heading.detail || presentation.subtitle || compactToolDetail(event),
        body: result.text || undefined,
        previewPath,
      };
    }
    case "commentary": {
      if (!event.body?.trim()) {
        return null;
      }
      return {
        type: "activity",
        id: event.id || `commentary:${event.seq}`,
        timestamp: event.timestamp,
        title: event.title || "Reasoning",
        tone: "running",
        icon: "ellipse-outline",
        body: event.body,
      };
    }
    case "status": {
      const title = [event.title, event.body].filter(Boolean).join(" · ");
      if (!title || isLowSignalStatus(title)) {
        return null;
      }
      return {
        type: "activity",
        id: event.id || `status:${event.seq}`,
        timestamp: event.timestamp,
        title,
        tone: "neutral",
        icon: "ellipse-outline",
      };
    }
    default:
      return null;
  }
}

function explorationEntryFromEvent(event: CodexConversationEvent): ExplorationEntry | null {
  const presentation = commandPresentation(event.command || "");
  if (!presentation.groupable) {
    return null;
  }
  const failed = isCommandFailed(event, presentation);
  const running = event.status === "running";
  return {
    event,
    presentation,
    running,
    failed,
    output: formatOutputPreview(event.body || "", {
      maxLines: 4,
      maxChars: 520,
    }),
  };
}

function explorationActivityFromEntries(
  entries: ExplorationEntry[],
): Extract<ZenTimelineItem, { type: "activity" }> {
  const first = entries[0];
  const last = entries[entries.length - 1] ?? first;
  const running = entries.some((entry) => entry.running);
  const failed = entries.some((entry) => entry.failed);
  const files = uniqueStrings(
    entries
      .map((entry) => entry.presentation.target)
      .filter((value): value is string => Boolean(value)),
  ).slice(0, 12);
  const commandLines = entries.map((entry) => explorationEntryLine(entry));
  const failedOutputs = entries
    .filter((entry) => entry.failed && entry.output.text)
    .flatMap((entry) => [
      "",
      `${entry.presentation.detail || commandSummary(entry.event.command || "") || "Command"} output:`,
      entry.output.text,
    ]);
  const body = cleanDisplayText([...commandLines, ...failedOutputs].join("\n"));
  const detail = summarizeExploration(entries);

  return {
    type: "activity",
    id: `explore:${first?.event.id || first?.event.seq}:${last?.event.id || last?.event.seq}`,
    timestamp: last?.event.timestamp || first?.event.timestamp,
    title: running ? "Exploring" : "Explored",
    tone: running ? "running" : failed ? "failed" : "success",
    icon: failed ? "alert-circle-outline" : running ? "time-outline" : "folder-open-outline",
    detail,
    body: body || undefined,
    files,
  };
}

function explorationEntryLine(entry: ExplorationEntry) {
  const action = entry.presentation.explorationLabel || entry.presentation.doneTitle;
  const target = entry.presentation.detail || commandSummary(entry.event.command || "") || "project";
  const suffix = entry.running ? " (running)" : entry.failed ? " (failed)" : "";
  return `${action} ${target}${suffix}`;
}

function summarizeExploration(entries: ExplorationEntry[]) {
  if (entries.length === 0) {
    return undefined;
  }
  const visibleTargets = uniqueStrings(
    entries
      .map((entry) => entry.presentation.target || entry.presentation.query)
      .filter((value): value is string => Boolean(value)),
  );
  if (visibleTargets.length > 0) {
    const summary = visibleTargets.slice(0, 2).map(shortPath).join(", ");
    const hidden = visibleTargets.length - 2;
    return hidden > 0 ? `${summary} +${hidden}` : summary;
  }
  return `${entries.length} lookup${entries.length === 1 ? "" : "s"}`;
}

function extractDisplayMessage(value: string): {
  body: string;
  attachments: DisplayAttachment[];
} {
  let body = cleanDisplayText(value);
  const attachments: DisplayAttachment[] = [];

  const tagMatch = ATTACHMENT_TAG_RE.exec(body);
  if (tagMatch) {
    attachments.push(...attachmentsFromTag(tagMatch[1]));
    body = cleanDisplayText(body.replace(tagMatch[0], ""));
  }

  const legacy = stripLegacyUploadedFiles(body);
  body = legacy.body;
  attachments.push(...legacy.attachments);

  return {
    body,
    attachments,
  };
}

function attachmentsFromTag(value: string): DisplayAttachment[] {
  try {
    const parsed = JSON.parse(value.trim());
    const files = Array.isArray(parsed?.files) ? parsed.files : [];
    return files
      .map((file: any) => ({
        name: typeof file?.name === "string" ? file.name.trim() : "",
        path: typeof file?.path === "string" ? file.path.trim() : "",
      }))
      .filter((file: DisplayAttachment) => file.path);
  } catch {
    return [];
  }
}

function stripLegacyUploadedFiles(value: string): {
  body: string;
  attachments: DisplayAttachment[];
} {
  const lines = value.split("\n");
  const keep: string[] = [];
  const attachments: DisplayAttachment[] = [];
  let consuming = false;

  for (const line of lines) {
    if (/^Uploaded files?:\s*$/i.test(line.trim())) {
      consuming = true;
      continue;
    }
    if (consuming) {
      const item = /^-\s*(.*?):\s*(\/\S.*)$/.exec(line.trim());
      if (item) {
        attachments.push({
          name: item[1].trim(),
          path: item[2].trim(),
        });
        continue;
      }
      if (!line.trim()) {
        continue;
      }
      consuming = false;
    }
    keep.push(line);
  }

  return {
    body: cleanDisplayText(keep.join("\n")),
    attachments,
  };
}

function cleanDisplayText(value: string) {
  return value.replace(/\n{3,}/g, "\n\n").trim();
}

function patchSummaryFromEvent(event: CodexConversationEvent): PatchSummary {
  const parsed = parseApplyPatchSummary(event.body || "");
  const fallbackFiles =
    parsed.files.length > 0
      ? parsed.files
      : (event.files ?? []).map((path) => ({
          path,
          operation: "update" as PatchOperation,
          added: 0,
          removed: 0,
        }));
  const files = fallbackFiles.sort((left, right) => left.path.localeCompare(right.path));
  const totalAdded = files.reduce((sum, file) => sum + file.added, 0);
  const totalRemoved = files.reduce((sum, file) => sum + file.removed, 0);
  const title = patchSummaryTitle(files, totalAdded, totalRemoved);
  return { title, files, totalAdded, totalRemoved };
}

function parseApplyPatchSummary(patch: string): PatchSummary {
  const files: PatchFileSummary[] = [];
  let current: PatchFileSummary | null = null;

  const finishCurrent = () => {
    if (!current) {
      return;
    }
    files.push(current);
    current = null;
  };

  for (const rawLine of patch.replace(/\r\n/g, "\n").split("\n")) {
    const line = rawLine.trimEnd();
    const add = /^\*\*\* Add File:\s+(.+)$/.exec(line);
    if (add) {
      finishCurrent();
      current = {
        path: add[1].trim(),
        operation: "add",
        added: 0,
        removed: 0,
      };
      continue;
    }
    const update = /^\*\*\* Update File:\s+(.+)$/.exec(line);
    if (update) {
      finishCurrent();
      current = {
        path: update[1].trim(),
        operation: "update",
        added: 0,
        removed: 0,
      };
      continue;
    }
    const del = /^\*\*\* Delete File:\s+(.+)$/.exec(line);
    if (del) {
      finishCurrent();
      current = {
        path: del[1].trim(),
        operation: "delete",
        added: 0,
        removed: 0,
      };
      continue;
    }
    const move = /^\*\*\* Move to:\s+(.+)$/.exec(line);
    if (move && current) {
      current.movePath = move[1].trim();
      continue;
    }
    if (!current || line.startsWith("***") || line.startsWith("@@")) {
      continue;
    }
    if (line.startsWith("+")) {
      current.added += 1;
    } else if (line.startsWith("-")) {
      current.removed += 1;
    }
  }
  finishCurrent();

  const totalAdded = files.reduce((sum, file) => sum + file.added, 0);
  const totalRemoved = files.reduce((sum, file) => sum + file.removed, 0);
  return {
    title: patchSummaryTitle(files, totalAdded, totalRemoved),
    files,
    totalAdded,
    totalRemoved,
  };
}

function patchSummaryTitle(files: PatchFileSummary[], totalAdded: number, totalRemoved: number) {
  if (files.length === 0) {
    return "Edited files";
  }
  if (files.length === 1) {
    const file = files[0];
    const verb =
      file.operation === "add" ? "Added" : file.operation === "delete" ? "Deleted" : "Edited";
    return `${verb} ${patchDisplayPath(file)} ${lineCountSummary(file.added, file.removed)}`;
  }
  return `Edited ${files.length} files ${lineCountSummary(totalAdded, totalRemoved)}`;
}

export function patchDisplayPath(file: PatchFileSummary) {
  return file.movePath ? `${file.path} -> ${file.movePath}` : file.path;
}

function lineCountSummary(added: number, removed: number) {
  return `(+${added} -${removed})`;
}

export function truncateRunes(value: string, limit: number) {
  const chars = Array.from(value);
  if (chars.length <= limit) {
    return value;
  }
  return chars.slice(0, Math.max(0, limit - 1)).join("") + "…";
}

function isLowSignalStatus(value: string) {
  return /^(Task started|Goal updated|Patch applied)$/i.test(value.trim());
}

function isLowSignalToolEvent(name: string, input: string) {
  const normalized = name.trim();
  if (normalized === "write_stdin" || normalized.endsWith(".write_stdin")) {
    try {
      const parsed = JSON.parse(input);
      return parsed?.chars === "";
    } catch {
      return false;
    }
  }
  return false;
}

export function isEventRunning(event: CodexConversationEvent) {
  return event.status === "running";
}

function toolActivityHeading(event: CodexConversationEvent, running: boolean) {
  const name = (event.tool_name || event.title || "tool").trim().replace(/^functions\./, "");
  if (name === "view_image") {
    return {
      title: running ? "Calling" : "Viewed Image",
      detail: compactToolDetail(event),
    };
  }
  if (name === "write_stdin") {
    const interaction = terminalInteractionHeading(event);
    if (interaction) {
      return interaction;
    }
  }
  return {
    title: running ? "Calling" : "Called",
    detail: toolInvocationLabel(event),
  };
}

function terminalInteractionHeading(event: CodexConversationEvent) {
  const input = parseToolPayload(event.input);
  const inputObject = isRecord(input) ? input : {};
  const chars = stringField(inputObject, "chars");
  const command = event.command || "command";
  if (!chars) {
    return {
      title: "Waited for",
      detail: commandSummary(command),
    };
  }
  const preview = truncateRunes(displayControlText(chars), 80);
  return {
    title: "Interacted with",
    detail: [commandSummary(command), preview ? `sent ${preview}` : ""].filter(Boolean).join(", "),
  };
}

function toolInvocationLabel(event: CodexConversationEvent) {
  const rawName = (event.tool_name || event.title || "tool").trim().replace(/^functions\./, "");
  const name = formatToolInvocationName(rawName);
  const input = parseToolPayload(event.input);
  const args = isRecord(input) ? compactToolInvocationArgs(input) : "";
  return `${name}(${args})`;
}

function formatToolInvocationName(name: string) {
  const mcpMatch = /^mcp__([^_]+(?:_[^_]+)*)__+(.+)$/.exec(name);
  if (mcpMatch) {
    return `${mcpMatch[1]}.${mcpMatch[2]}`;
  }
  return name || "tool";
}

function compactToolInvocationArgs(record: Record<string, unknown>) {
  const hidden = new Set(["max_output_tokens", "yield_time_ms", "timeout_ms", "response_length"]);
  const compact: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(record)) {
    if (hidden.has(key)) {
      continue;
    }
    compact[key] = value;
    if (Object.keys(compact).length >= 3) {
      break;
    }
  }
  const text = Object.keys(compact).length > 0 ? JSON.stringify(compact) : "";
  return truncateRunes(text, 120);
}

function compactToolDetail(event: CodexConversationEvent) {
  const parsed = parseToolPayload(event.input);
  if (!isRecord(parsed)) {
    return (event.tool_name || "").trim().replace(/^functions\./, "");
  }
  return (
    stringField(parsed, "path") ||
    stringField(parsed, "url") ||
    stringField(parsed, "target") ||
    stringField(parsed, "query") ||
    (event.tool_name || "").trim().replace(/^functions\./, "")
  );
}

function imagePathFromTool(event: CodexConversationEvent) {
  const parsed = parseToolPayload(event.input);
  if (!isRecord(parsed)) {
    return undefined;
  }
  const path = stringField(parsed, "path") || stringField(parsed, "image_url");
  if (!path || previewableImageUri(path) || !looksLikeImagePath(path)) {
    return undefined;
  }
  return path;
}

function commandPresentation(command: string): CommandPresentation {
  const normalized = cleanDisplayText(command);
  const firstLine = normalized.split("\n").find((line) => line.trim())?.trim() || "";
  const tokens = commandTokens(firstLine);
  const executable = commandExecutable(tokens);
  const lower = firstLine.toLowerCase();
  const fallbackDetail = commandSummary(command);

  if (["cat", "sed", "nl", "less", "head", "tail"].includes(executable)) {
    const target = commandTarget(tokens, executable);
    return {
      kind: "read",
      target,
      detail: target || fallbackDetail,
      icon: "document-text-outline",
      runningTitle: "Reading file",
      doneTitle: "Read file",
      failedTitle: "Read failed",
      groupable: true,
      explorationLabel: "Read",
    };
  }

  if (executable === "ls" || (executable === "find" && !/\s-name\s|\s-iname\s|\s-type\s+f/.test(lower))) {
    const target = commandTarget(tokens, executable) || ".";
    return {
      kind: "list",
      target,
      detail: target,
      icon: "folder-open-outline",
      runningTitle: "Listing files",
      doneTitle: "Listed files",
      failedTitle: "List failed",
      groupable: true,
      explorationLabel: "List",
    };
  }

  if (["rg", "grep", "ag", "ack"].includes(executable) || executable === "find") {
    const query = searchQuery(tokens, executable);
    const target = searchTarget(tokens, executable);
    const detail = [query ? truncateRunes(query, 36) : "", target].filter(Boolean).join(" in ");
    return {
      kind: "search",
      query,
      target,
      detail: detail || fallbackDetail,
      icon: "search-outline",
      runningTitle: "Searching project",
      doneTitle: "Searched project",
      failedTitle: "Search failed",
      groupable: true,
      explorationLabel: "Search",
    };
  }

  if (/\b(go test|bun test|npm test|pnpm test|yarn test|jest|vitest|pytest)\b/.test(lower)) {
    return {
      kind: "test",
      detail: fallbackDetail,
      icon: "checkmark-done-outline",
      runningTitle: "Running",
      doneTitle: "Ran",
      failedTitle: "Ran",
      groupable: false,
    };
  }

  if (/\b(tsc|lint|typecheck|doctor|gradlew|xcodebuild|build|assemble)\b/.test(lower)) {
    return {
      kind: "check",
      detail: fallbackDetail,
      icon: "construct-outline",
      runningTitle: "Running",
      doneTitle: "Ran",
      failedTitle: "Ran",
      groupable: false,
    };
  }

  if (/\bgit\b/.test(lower)) {
    return {
      kind: "git",
      detail: fallbackDetail,
      icon: "git-branch-outline",
      runningTitle: "Running",
      doneTitle: "Ran",
      failedTitle: "Ran",
      groupable: false,
    };
  }

  if (/\b(bun install|npm install|pnpm install|yarn install)\b/.test(lower)) {
    return {
      kind: "install",
      detail: fallbackDetail,
      icon: "download-outline",
      runningTitle: "Running",
      doneTitle: "Ran",
      failedTitle: "Ran",
      groupable: false,
    };
  }

  return {
    kind: "run",
    detail: fallbackDetail,
    icon: "terminal-outline",
    runningTitle: "Running",
    doneTitle: "Ran",
    failedTitle: "Ran",
    groupable: false,
  };
}

function commandActivityTitle(
  command: string,
  running: boolean,
  failed: boolean,
  presentation: CommandPresentation = commandPresentation(command),
) {
  void command;
  void failed;
  void presentation;
  return running ? "Running" : "Ran";
}

function commandSummary(command: string) {
  command = cleanDisplayText(command);
  if (!command) {
    return undefined;
  }
  const firstLine = command.split("\n")[0];
  return truncateRunes(firstLine, 72);
}

function tokenizeShellLike(value: string): string[] {
  const tokens: string[] = [];
  let current = "";
  let quote: "'" | '"' | "" = "";
  let escaping = false;

  for (const char of value) {
    if (escaping) {
      current += char;
      escaping = false;
      continue;
    }
    if (char === "\\") {
      escaping = true;
      continue;
    }
    if (quote) {
      if (char === quote) {
        quote = "";
      } else {
        current += char;
      }
      continue;
    }
    if (char === "'" || char === '"') {
      quote = char;
      continue;
    }
    if (/\s/.test(char)) {
      if (current) {
        tokens.push(current);
        current = "";
      }
      continue;
    }
    current += char;
  }

  if (current) {
    tokens.push(current);
  }
  return tokens;
}

function commandTokens(value: string): string[] {
  const tokens = tokenizeShellLike(value);
  const executable = basename(tokens[0] || "").toLowerCase();
  if (executable === "bash" || executable === "sh" || executable === "zsh") {
    const commandIndex = tokens.findIndex((token) => token === "-c" || token === "-lc");
    if (commandIndex >= 0 && tokens[commandIndex + 1]) {
      return commandTokens(tokens[commandIndex + 1]);
    }
  }
  return tokens;
}

function commandExecutable(tokens: string[]) {
  const executableTokens = tokens.filter((token) => token !== "env");
  while (executableTokens[0]?.includes("=")) {
    executableTokens.shift();
  }
  const executable = executableTokens[0] || "";
  return basename(executable).toLowerCase();
}

function commandTarget(tokens: string[], executable: string) {
  const positional = commandPositionals(tokens, executable);
  if (positional.length === 0) {
    return "";
  }
  if (executable === "sed") {
    return positional.find((token) => !/^\d*,?\d*p$/.test(token) && !/^s[|/]/.test(token)) || positional[positional.length - 1];
  }
  if (executable === "find") {
    return positional[0];
  }
  return positional[positional.length - 1];
}

function commandPositionals(tokens: string[], executable: string) {
  const start = tokens.findIndex((token) => basename(token).toLowerCase() === executable);
  const relevant = start >= 0 ? tokens.slice(start + 1) : tokens.slice(1);
  const positionals: string[] = [];
  for (let index = 0; index < relevant.length; index++) {
    const token = relevant[index];
    if (!token || token === "--") {
      continue;
    }
    if (token.startsWith("-")) {
      const optionTakesValue =
        [
          "-e",
          "-f",
          "-g",
          "--glob",
          "--type",
          "-t",
          "-m",
          "--max-count",
          "-C",
          "-A",
          "-B",
        ].includes(token) && relevant[index + 1] && !relevant[index + 1].startsWith("-");
      if (optionTakesValue) {
        index++;
      }
      continue;
    }
    if (token.includes("=") && positionals.length === 0) {
      continue;
    }
    positionals.push(token);
  }
  return positionals;
}

function searchQuery(tokens: string[], executable: string) {
  const positionals = commandPositionals(tokens, executable);
  if (executable === "find") {
    const nameIndex = tokens.findIndex((token) => token === "-name" || token === "-iname");
    return nameIndex >= 0 ? tokens[nameIndex + 1] || "" : positionals.slice(1).join(" ");
  }
  return positionals[0] || "";
}

function searchTarget(tokens: string[], executable: string) {
  const positionals = commandPositionals(tokens, executable);
  if (executable === "find") {
    return positionals[0] || ".";
  }
  return positionals.slice(1).join(", ");
}

function isCommandFailed(event: CodexConversationEvent, presentation: CommandPresentation) {
  if (event.status === "failed" || (event.exit_code ?? 0) !== 0) {
    if (presentation.kind === "search" && event.exit_code === 1 && !cleanToolOutput(event.body || "")) {
      return false;
    }
    return true;
  }
  return false;
}

function cleanToolOutput(value: string) {
  value = cleanDisplayText(value);
  if (!value) {
    return "";
  }
  const lines = value.split("\n");
  const outputLine = lines.findIndex((line) => line.trim() === "Output:");
  const bodyLines = outputLine >= 0 ? lines.slice(outputLine + 1) : lines;
  return cleanDisplayText(bodyLines.filter((line) => !isToolMetadataLine(line)).join("\n"));
}

function formatOutputPreview(value: string, options: OutputPreviewOptions): OutputPreview {
  let output = cleanToolOutput(value);
  if (!output) {
    return { text: "", truncated: false };
  }

  output = compactJsonForPreview(output);
  const charLimited = truncateOutputChars(output, options.maxChars);
  const lineLimited = truncateOutputLines(charLimited.text, options.maxLines);
  return {
    text: lineLimited.text,
    truncated: charLimited.truncated || lineLimited.truncated,
  };
}

function compactJsonForPreview(value: string) {
  const trimmed = value.trim();
  if (!/^[\[{]/.test(trimmed)) {
    return value;
  }
  try {
    const parsed = JSON.parse(trimmed);
    const compact = JSON.stringify(parsed);
    return compact
      .replace(/":/g, '": ')
      .replace(/,"/g, ', "');
  } catch {
    return value;
  }
}

function truncateOutputChars(value: string, maxChars: number): OutputPreview {
  const chars = Array.from(value);
  if (chars.length <= maxChars) {
    return { text: value, truncated: false };
  }
  const headCount = Math.max(120, Math.floor(maxChars * 0.58));
  const tailCount = Math.max(80, maxChars - headCount - 80);
  const hidden = chars.length - headCount - tailCount;
  return {
    text: cleanDisplayText(
      [
        chars.slice(0, headCount).join(""),
        `... ${hidden} chars hidden. ${FULL_OUTPUT_HINT}`,
        chars.slice(chars.length - tailCount).join(""),
      ].join("\n"),
    ),
    truncated: true,
  };
}

function truncateOutputLines(value: string, maxLines: number): OutputPreview {
  const lines = value.split("\n");
  if (lines.length <= maxLines) {
    return { text: value, truncated: false };
  }
  const headCount = Math.max(1, Math.ceil(maxLines / 2));
  const tailCount = Math.max(1, Math.floor(maxLines / 2));
  const hidden = lines.length - headCount - tailCount;
  return {
    text: cleanDisplayText(
      [
        ...lines.slice(0, headCount),
        `... +${hidden} lines hidden. ${FULL_OUTPUT_HINT}`,
        ...lines.slice(lines.length - tailCount),
      ].join("\n"),
    ),
    truncated: true,
  };
}

function isToolMetadataLine(line: string) {
  const trimmed = line.trim();
  return (
    trimmed.startsWith("Chunk ID:") ||
    trimmed.startsWith("Wall time:") ||
    trimmed.startsWith("Exit code:") ||
    trimmed.startsWith("Process exited with code ") ||
    trimmed.startsWith("Process running with session ID ") ||
    trimmed.startsWith("Original token count:") ||
    trimmed.startsWith("Total output lines:")
  );
}

function toolPresentation(event: CodexConversationEvent): ToolPresentation {
  const name = (event.tool_name || event.title || "tool").trim() || "tool";
  const input = parseToolPayload(event.input);
  const inputObject = isRecord(input) ? input : {};
  const browserAction = /^browser_/.test(name) ? humanizeToolName(name.replace(/^browser_/, "")) : "";

  if (name === "view_image") {
    const path = stringField(inputObject, "path") || stringField(inputObject, "image_url");
    const previewUri = previewableImageUri(path);
    return {
      subtitle: path ? basename(path) : undefined,
      icon: "image-outline",
      localImagePath: path && !previewUri ? path : undefined,
    };
  }

  if (name === "write_stdin") {
    const chars = stringField(inputObject, "chars");
    const sessionId = valueField(inputObject, "session_id");
    return {
      subtitle: sessionId ? `session ${sessionId}` : undefined,
      icon: chars === ""
        ? "sync-outline"
        : chars === "\u0003"
          ? "stop-circle-outline"
          : "return-down-forward-outline",
    };
  }

  if (browserAction) {
    const browserFile = stringField(inputObject, "filename") || firstString(inputObject.paths);
    const browserPreviewUri = looksLikeImagePath(browserFile)
      ? previewableImageUri(browserFile)
      : undefined;
    return {
      subtitle: stringField(inputObject, "element") || stringField(inputObject, "url") || undefined,
      icon: browserToolIcon(name),
      localImagePath: browserFile && !browserPreviewUri && looksLikeImagePath(browserFile)
        ? browserFile
        : undefined,
    };
  }

  if (name.includes("query_docs") || name.includes("resolve_library_id")) {
    return {
      subtitle: stringField(inputObject, "libraryId") || stringField(inputObject, "libraryName") || undefined,
      icon: "library-outline",
    };
  }

  if (name.includes("search_query") || name === "web.run") {
    return {
      icon: "search-outline",
    };
  }

  if (name.includes("multi_tool_use.parallel")) {
    const toolUses = Array.isArray(inputObject.tool_uses) ? inputObject.tool_uses : [];
    const names = toolUses
      .map((toolUse) =>
        isRecord(toolUse) && typeof toolUse.recipient_name === "string"
          ? humanizeToolName(toolUse.recipient_name)
          : "",
      )
      .filter(Boolean);
    return {
      subtitle: names.length ? names.slice(0, 2).join(", ") : undefined,
      icon: "git-network-outline",
    };
  }

  if (name.includes("spawn_agent") || name.includes("send_input") || name.includes("wait_agent")) {
    return {
      subtitle: stringField(inputObject, "target") || firstString(inputObject.targets),
      icon: "git-network-outline",
    };
  }

  return {
    icon: "cube-outline",
  };
}

function parseToolPayload(value?: string): unknown {
  if (!value) {
    return null;
  }
  try {
    return JSON.parse(value);
  } catch {
    return null;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function stringField(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  return typeof value === "string" ? value.trim() : "";
}

function valueField(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  if (typeof value === "string") {
    return value.trim();
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return "";
}

function firstString(value: unknown): string {
  return Array.isArray(value) && typeof value[0] === "string" ? value[0] : "";
}

function displayControlText(value: string): string {
  if (!value) {
    return "";
  }
  return value
    .replace(/\u0003/g, "Ctrl-C")
    .replace(/\n/g, "\\n")
    .replace(/\t/g, "\\t");
}

function humanizeToolName(value: string): string {
  return value
    .replace(/^mcp__/, "")
    .replace(/^functions\./, "")
    .replace(/__/g, " ")
    .replace(/_/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/\b\w/g, (letter) => letter.toUpperCase()) || "Tool";
}

function browserToolIcon(name: string): TimelineIconName {
  if (name.includes("navigate")) {
    return "navigate-outline";
  }
  if (name.includes("click")) {
    return "radio-button-on-outline";
  }
  if (name.includes("type") || name.includes("fill")) {
    return "text-outline";
  }
  if (name.includes("screenshot")) {
    return "camera-outline";
  }
  if (name.includes("snapshot")) {
    return "scan-outline";
  }
  return "globe-outline";
}

function previewableImageUri(value?: string) {
  if (!value) {
    return undefined;
  }
  if (/^(https?:|data:image\/|file:)/.test(value)) {
    return value;
  }
  return undefined;
}

function looksLikeImagePath(value: string) {
  return /\.(png|jpe?g|gif|webp|bmp)$/i.test(value.trim());
}

function basename(value: string) {
  const parts = value.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || value;
}

function shortPath(value: string) {
  const trimmed = value.trim();
  if (!trimmed) {
    return "";
  }
  const parts = trimmed.split(/[\\/]/).filter(Boolean);
  if (parts.length <= 2) {
    return trimmed;
  }
  return `${parts[parts.length - 2]}/${parts[parts.length - 1]}`;
}

function uniqueStrings(values: string[]) {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const value of values) {
    const normalized = value.trim();
    if (!normalized || seen.has(normalized)) {
      continue;
    }
    seen.add(normalized);
    result.push(normalized);
  }
  return result;
}
