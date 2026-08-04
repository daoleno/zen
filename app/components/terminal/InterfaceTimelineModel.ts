import type {
  CodexConversation,
  CodexConversationEvent,
  ProviderActivity,
} from "../../services/codexConversation";
import {
  buildExpandedToolDetails,
  isWaitSessionPoll,
} from "../../services/toolCallDetails";
import {
  collapsedToolLabel,
  isExecWrapperToolName,
  isUnsafeCollapsedDetail,
  parseExecWrapperCalls,
  primarySemanticAction,
  type SemanticAction,
  type SemanticActionKind,
} from "../../services/toolCallSemantics";
import { parseHeartbeatWakeMessage } from "./CodexHeartbeatWake";
import type { PendingUserMessage } from "./InterfaceChatSession";
import { presentPendingUserMessageLifecycle } from "./pendingUserMessageLifecycle";
import {
  type PatchFileSummary,
  type PatchOperation,
  type ZenActivityChild,
  type ZenActivityTimelineItem,
} from "./InterfaceTimelineActivityTypes";
import type { DisplayAttachment } from "./InterfaceTimelineMessage";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import { compareConversationEvents } from "./interfaceConversationReconciliation";

const ATTACHMENT_TAG_RE =
  /<zen_attachments>\s*([\s\S]*?)\s*<\/zen_attachments>/i;
const COMMAND_OUTPUT_PREVIEW_LINES = 7;
const COMMAND_OUTPUT_PREVIEW_CHARS = 1200;
const TOOL_PAYLOAD_PREVIEW_LINES = 6;
const TOOL_PAYLOAD_PREVIEW_CHARS = 1000;
const FULL_OUTPUT_HINT = "Expand this item for full output.";

type TimelineIconName = ZenActivityTimelineItem["icon"];

type ToolPresentation = {
  subtitle?: string;
  icon: TimelineIconName;
  localImagePath?: string;
};

type CommandKind =
  "read" | "list" | "search" | "test" | "check" | "git" | "install" | "run";

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
  totalAdded?: number;
  totalRemoved?: number;
};

export function buildZenTimeline(
  events: CodexConversationEvent[],
  turnFocusAnchorAliases?: ReadonlyMap<string, string>,
): ZenTimelineItem[] {
  const items: ZenTimelineItem[] = [];
  let explorationEntries: ExplorationEntry[] = [];

  const flushExploration = () => {
    if (explorationEntries.length === 0) {
      return;
    }
    items.push(explorationActivityFromEntries(explorationEntries));
    explorationEntries = [];
  };

  for (const event of [...events].sort(compareConversationEvents)) {
    if (event.kind === "user_message" || event.kind === "assistant_message") {
      flushExploration();
      const extracted = extractDisplayMessage(event.body || "");
      const heartbeatWake =
        event.kind === "user_message"
          ? parseHeartbeatWakeMessage(extracted.body)
          : null;
      if (
        !extracted.body &&
        extracted.attachments.length === 0 &&
        !heartbeatWake
      ) {
        continue;
      }
      const turnFocusAnchorId =
        event.kind === "user_message"
          ? turnFocusAnchorAliases?.get(event.id)
          : undefined;
      items.push({
        type: "message",
        id: event.id || `${event.kind}:${event.seq}`,
        role: event.kind === "user_message" ? "user" : "assistant",
        timestamp: event.timestamp,
        body: heartbeatWake ? "" : extracted.body,
        attachments: extracted.attachments,
        streaming: event.partial,
        heartbeatWake: heartbeatWake || undefined,
        ...(turnFocusAnchorId ? { turnFocusAnchorId } : {}),
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

    if (event.kind === "status") {
      flushExploration();
      if (shouldRenderStatusAsMessage(event)) {
        const body = (event.body || "").trim();
        if (body) {
          items.push({
            type: "message",
            id: event.id || `status:${event.seq}`,
            role: "assistant",
            timestamp: event.timestamp,
            body,
            attachments: [],
            streaming: event.partial,
          });
        }
      } else {
        const activity = activityFromEvent(event);
        if (activity) {
          items.push(activity);
        }
      }
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

    if (
      event.kind === "tool" &&
      isWaitSessionPoll(event.tool_name || event.title || "", event.input || "")
    ) {
      attachWaitStatusToLastCommand(items, event);
      continue;
    }
    const activity = activityFromEvent(event);
    if (activity) {
      items.push(activity);
    }
  }
  flushExploration();
  return items;
}

function attachWaitStatusToLastCommand(
  items: ZenTimelineItem[],
  event: CodexConversationEvent,
) {
  const details = buildExpandedToolDetails({
    toolName: event.tool_name || event.title,
    input: event.input,
    output: event.output || event.body,
    command: event.command,
    status: event.status,
    exitCode: event.exit_code,
    semanticKind: "wait",
  });
  const statusLine = details.statusLine;
  if (!statusLine) {
    return;
  }
  // Prefer linked in-progress exploration / last command activity.
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const item = items[index];
    if (item.type !== "activity") {
      continue;
    }
    if (
      item.tone === "running" ||
      item.statusKey === "running" ||
      item.statusKey === "done"
    ) {
      items[index] = {
        ...item,
        statusLine: item.statusLine || statusLine,
        detail: item.detail || statusLine,
      };
      return;
    }
  }
}

export function mergePendingUserMessagesIntoTimeline(
  timelineItems: ZenTimelineItem[],
  pendingUserMessages: PendingUserMessage[],
  onRetryPendingUserMessage?: (id: string) => void,
): ZenTimelineItem[] {
  if (pendingUserMessages.length === 0) {
    return timelineItems;
  }
  const merged = [...timelineItems];
  const placedLocalRowIDs = new Set<string>();
  for (const message of pendingUserMessages) {
    const item = {
      type: "message" as const,
      id: message.id,
      role: "user" as const,
      timestamp: message.createdAt,
      body: message.body,
      attachments: message.attachments,
    };
    const presentation = presentPendingUserMessageLifecycle(message);

    const pendingItem = {
      ...item,
      pending: true,
      pendingLifecycle: message.lifecycle,
      pendingLifecycleLabel: presentation.label,
      pendingLifecycleAccessibilityLabel: presentation.accessibilityLabel,
      pendingFailureMessage:
        message.lifecycle === "failed" ? message.failureMessage : undefined,
      onRetryPending:
        message.lifecycle === "failed" && onRetryPendingUserMessage
          ? () => onRetryPendingUserMessage(message.id)
          : undefined,
    };

    insertPendingCurrentAtCausalBoundary(
      merged,
      pendingItem,
      message,
      placedLocalRowIDs,
    );
    placedLocalRowIDs.add(pendingItem.id);
  }
  return merged;
}

export function mergeSupplementaryTimelineItems(
  timelineItems: ZenTimelineItem[],
  supplementaryItems: ZenTimelineItem[],
): ZenTimelineItem[] {
  if (supplementaryItems.length === 0) {
    return timelineItems;
  }
  const merged = [...timelineItems];
  const seen = new Set(merged.map((item) => item.id));
  for (const item of supplementaryItems) {
    if (seen.has(item.id)) {
      continue;
    }
    seen.add(item.id);
    insertTimelineItemByTimestamp(merged, item);
  }
  return merged;
}

export function supplementaryTimelineItemsForConversation({
  items,
  conversationScopeKey,
  conversation,
  loading,
}: {
  items: ZenTimelineItem[];
  conversationScopeKey?: string;
  conversation: CodexConversation | null;
  loading: boolean;
}): ZenTimelineItem[] {
  if (!conversationScopeKey || items.length === 0) {
    return items;
  }
  if (loading && !conversation) {
    return [];
  }
  // A matching retained snapshot remains canonical while its subscription
  // reconnects, even when the session reports loading again. Conversely, an
  // accepted empty snapshot is ready because it still carries this identity.
  return conversation?.session_id === conversationScopeKey ? items : [];
}

function insertPendingCurrentAtCausalBoundary(
  timelineItems: ZenTimelineItem[],
  item: ZenTimelineItem,
  message: PendingUserMessage,
  placedLocalRowIDs: ReadonlySet<string>,
) {
  const previousEventIds = new Set(message.createdAfterEventIds ?? []);
  let lastPreviousIndex = -1;
  timelineItems.forEach((candidate, index) => {
    if (previousEventIds.has(candidate.id)) {
      lastPreviousIndex = index;
    }
  });
  if (lastPreviousIndex >= 0) {
    let insertAt = lastPreviousIndex + 1;
    while (
      insertAt < timelineItems.length &&
      placedLocalRowIDs.has(timelineItems[insertAt].id)
    ) {
      insertAt += 1;
    }
    timelineItems.splice(insertAt, 0, item);
    return;
  }
  insertTimelineItemByTimestamp(timelineItems, item);
}

export function mergeRunningActivityIntoTimeline(
  timelineItems: ZenTimelineItem[],
  activity?: ProviderActivity,
) {
  if (activity?.status !== "running") {
    return timelineItems;
  }
  const id = `provider-activity:${activity.id}`;
  if (timelineItems.some((item) => item.id === id)) {
    return timelineItems;
  }
  const merged = [...timelineItems];
  merged.push({
    type: "activity",
    id,
    title: "Working",
    tone: "running",
    icon: "time-outline",
    statusKey: "running",
    defaultExpanded: false,
  });
  return merged;
}

function insertTimelineItemByTimestamp(
  timelineItems: ZenTimelineItem[],
  item: ZenTimelineItem,
) {
  const timestamp = item.timestamp
    ? new Date(item.timestamp).getTime()
    : Number.NaN;
  if (!Number.isFinite(timestamp)) {
    timelineItems.push(item);
    return;
  }
  const insertAt = timelineItems.findIndex((candidate) => {
    if (candidate.id.startsWith("provider-activity:")) {
      return true;
    }
    if (!candidate.timestamp) {
      return false;
    }
    const candidateTimestamp = new Date(candidate.timestamp).getTime();
    return (
      Number.isFinite(candidateTimestamp) && candidateTimestamp > timestamp
    );
  });
  if (insertAt < 0) {
    timelineItems.push(item);
    return;
  }
  timelineItems.splice(insertAt, 0, item);
}

function shouldRenderStatusAsMessage(event: CodexConversationEvent) {
  return (
    event.kind === "status" &&
    (event.title || "").trim() === "Codex" &&
    !(event.status || "").trim() &&
    Boolean((event.body || "").trim())
  );
}

function activityFromEvent(
  event: CodexConversationEvent,
): ZenTimelineItem | null {
  switch (event.kind) {
    case "command": {
      const presentation = commandPresentation(event.command || "");
      const failed = isCommandFailed(event, presentation);
      const running = isEventRunning(event);
      const command = event.command || "";
      const status = running
        ? "running"
        : failed
          ? "failed"
          : event.status || "done";
      const semantic = collapsedToolLabel({
        kind: "command",
        command,
        toolName: "exec_command",
        status,
        exitCode: event.exit_code,
      });
      const action = primarySemanticAction({
        kind: "command",
        command,
        toolName: "exec_command",
        status,
        exitCode: event.exit_code,
      });
      const details = buildExpandedToolDetails({
        kind: "command",
        toolName: "exec_command",
        command,
        output: event.body,
        status,
        exitCode: event.exit_code,
        semanticKind: action.kind,
        files: presentation.target ? [presentation.target] : event.files,
      });
      const output = formatOutputPreview(details.result || event.body || "", {
        maxLines: COMMAND_OUTPUT_PREVIEW_LINES,
        maxChars: COMMAND_OUTPUT_PREVIEW_CHARS,
      });
      return {
        type: "activity",
        id: event.id || `command:${event.seq}`,
        timestamp: event.timestamp,
        statusKey: event.status || "done",
        title: semantic.title,
        tone: running ? "running" : failed ? "failed" : "success",
        icon: running
          ? "time-outline"
          : failed
            ? "alert-circle-outline"
            : presentation.icon,
        detail: safeCollapsedDetail(details.quietDetail || semantic.detail),
        body: output.text || undefined,
        bodyKind: output.text
          ? commandOutputBodyKind(command, output.text)
          : undefined,
        commandText: details.command,
        queryText: details.query,
        statusLine: details.statusLine,
        files: details.files,
        defaultExpanded: failed,
        accessibilityLabel: semantic.accessibilityLabel,
        providerToolId:
          action.kind === "read_files" || action.kind === "search_code"
            ? undefined
            : details.developer?.providerToolId,
        developerDetails:
          action.kind === "read_files" || action.kind === "search_code"
            ? undefined
            : details.developer,
      };
    }
    case "patch": {
      const summary = patchSummaryFromEvent(event);
      const semantic = collapsedToolLabel({
        kind: "patch",
        toolName: "apply_patch",
        files: summary.files.map((file) => file.path),
        status: "done",
      });
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
        defaultExpanded: false,
        accessibilityLabel: summary.title,
        providerToolId: semantic.providerToolId,
        developerDetails: semantic.providerToolId
          ? { providerToolId: semantic.providerToolId }
          : undefined,
      };
    }
    case "tool": {
      const name = event.tool_name || event.title || "tool";
      if (isLowSignalToolEvent(name, event.input || "")) {
        return null;
      }
      const failed =
        isFailureLikeStatus(event.status) || (event.exit_code ?? 0) !== 0;
      const running = isEventRunning(event);
      const status = running
        ? "running"
        : failed
          ? "failed"
          : event.status || "done";
      const presentation = toolPresentation(event);
      const previewPath =
        presentation.localImagePath || imagePathFromTool(event);
      const semantic = collapsedToolLabel({
        kind: "tool",
        toolName: name,
        title: event.title,
        input: event.input,
        command: event.command,
        status,
        exitCode: event.exit_code,
        files: event.files,
      });
      const action = primarySemanticAction({
        kind: "tool",
        toolName: name,
        title: event.title,
        input: event.input,
        command: event.command,
        status,
        exitCode: event.exit_code,
        files: event.files,
      });
      const details = buildExpandedToolDetails({
        kind: "tool",
        toolName: name,
        title: event.title,
        input: event.input,
        output: event.output || event.body,
        command: event.command,
        status,
        exitCode: event.exit_code,
        files: event.files,
        semanticKind: action.kind,
      });
      if (details.hideCard || details.mergeIntoCommand) {
        return null;
      }
      const result = formatOutputPreview(details.result || "", {
        maxLines: TOOL_PAYLOAD_PREVIEW_LINES,
        maxChars: TOOL_PAYLOAD_PREVIEW_CHARS,
      });
      const isWait = action.kind === "wait";
      const developerDetails =
        isWait || action.kind === "read_files" || action.kind === "search_code"
          ? details.developer
          : details.developer
            ? {
                ...details.developer,
                providerToolId:
                  semantic.providerToolId || details.developer.providerToolId,
              }
            : semantic.providerToolId
              ? { providerToolId: semantic.providerToolId }
              : undefined;
      return {
        type: "activity",
        id: event.id || `tool:${event.seq}`,
        timestamp: event.timestamp,
        statusKey: event.status || "done",
        title: isWait ? (running ? "Waiting" : "Finished") : semantic.title,
        tone: running ? "running" : failed ? "failed" : "success",
        icon: semanticActivityIcon(
          semantic.title,
          presentation.icon,
          running,
          failed,
        ),
        detail: safeCollapsedDetail(
          isWait
            ? details.statusLine || details.quietDetail
            : details.quietDetail || semantic.detail,
        ),
        body: isWait ? undefined : result.text || undefined,
        bodyKind:
          !isWait && result.text
            ? toolOutputBodyKind(event, result.text)
            : undefined,
        commandText: isWait ? undefined : details.command,
        queryText: details.query,
        statusLine: details.statusLine,
        files: details.files,
        previewPath,
        defaultExpanded: failed,
        accessibilityLabel: isWait
          ? details.statusLine || (running ? "Waiting" : "Finished")
          : semantic.accessibilityLabel,
        providerToolId:
          action.kind === "read_files" ||
          action.kind === "search_code" ||
          isWait
            ? undefined
            : developerDetails?.providerToolId,
        developerDetails,
        children: semanticChildren(
          event.id || `tool:${event.seq}`,
          semantic.children,
        ),
      };
    }
    case "web_search": {
      const failed =
        isFailureLikeStatus(event.status) || (event.exit_code ?? 0) !== 0;
      const running = isEventRunning(event);
      const action = parseToolPayload(event.input);
      const body = webSearchActivityBody(event);
      return {
        type: "activity",
        id: event.id || `web-search:${event.seq}`,
        timestamp: event.timestamp,
        statusKey: event.status || "done",
        title: webSearchActivityTitle(action, running, failed),
        tone: running ? "running" : failed ? "failed" : "success",
        icon: "search-outline",
        detail: webSearchEventDetail(event),
        body,
        bodyKind: body ? "terminal" : undefined,
        defaultExpanded: failed,
      };
    }
    case "commentary": {
      if (!event.body?.trim()) {
        return null;
      }
      const running = isEventRunning(event);
      return {
        type: "activity",
        id: event.id || `commentary:${event.seq}`,
        timestamp: event.timestamp,
        title: event.title || "Reasoning",
        statusKey: event.status || "done",
        tone: running ? "running" : "neutral",
        icon: running ? "time-outline" : "bulb",
        activityKind: "reasoning",
        streaming: event.partial,
        body: event.body,
        defaultExpanded: running,
      };
    }
    case "status": {
      const calendarResult = event.source === "calendar_result";
      const running = isEventRunning(event);
      const title =
        cleanTerminalDisplayText(event.title || "") ||
        statusActivityTitle(event.status);
      const body = cleanTerminalDisplayText(event.body || "");
      if ((!title && !body) || (isLowSignalStatus(title) && !body)) {
        return null;
      }
      const detail = statusActivityDetail(body);
      return {
        type: "activity",
        id: event.id || `status:${event.seq}`,
        timestamp: event.timestamp,
        statusKey: event.status || "done",
        title: title || "Agent status",
        tone: running
          ? "running"
          : calendarResult && event.status !== "failed"
            ? "success"
            : statusActivityTone(event.status),
        icon: calendarResult
          ? "calendar-outline"
          : statusActivityIcon(running ? "running" : event.status),
        streaming: event.partial,
        detail,
        body: body || undefined,
        bodyKind: calendarResult
          ? undefined
          : statusActivityBodyKind(event, body),
        defaultExpanded:
          running || event.status === "failed" || event.status === "blocked",
      };
    }
    default:
      return null;
  }
}

function explorationEntryFromEvent(
  event: CodexConversationEvent,
): ExplorationEntry | null {
  const presentation = commandPresentation(event.command || "");
  if (!presentation.groupable) {
    return null;
  }
  const failed = isCommandFailed(event, presentation);
  const running = isEventRunning(event);
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
      `${entry.presentation.explorationLabel || "Lookup"} output:`,
      entry.output.text,
    ]);
  const body =
    failedOutputs.length > 0
      ? cleanTerminalDisplayText([...commandLines, ...failedOutputs].join("\n"))
      : "";
  const semantic = collapsedToolLabel({
    kind: "command",
    command: first?.event.command,
    toolName: "exec_command",
    status: running ? "running" : failed ? "failed" : "done",
  });
  const title =
    entries.length > 1 ? (running ? "Exploring" : "Explored") : semantic.title;
  const quietDetail =
    entries.length > 1
      ? `${entries.length} lookups`
      : files.length === 1
        ? basename(files[0])
        : files.length > 1
          ? `${files.length} files`
          : undefined;

  return {
    type: "activity",
    id: `explore:${first?.event.id || first?.event.seq}`,
    timestamp: last?.event.timestamp || first?.event.timestamp,
    statusKey: running ? "running" : failed ? "failed" : "done",
    title,
    tone: running ? "running" : failed ? "failed" : "success",
    icon: failed
      ? "alert-circle-outline"
      : running
        ? "time-outline"
        : "folder-open-outline",
    detail: quietDetail,
    body: body || undefined,
    files,
    defaultExpanded: false,
    accessibilityLabel:
      entries.length > 1
        ? `${title}, ${entries.length} lookups`
        : semantic.accessibilityLabel,
    providerToolId: undefined,
    developerDetails: undefined,
    children:
      entries.length > 1
        ? entries.map((entry, index) => ({
            id: `${first?.event.id || "explore"}:child:${index}`,
            title: collapsedToolLabel({
              kind: "command",
              command: entry.event.command,
              toolName: "exec_command",
              status: entry.running
                ? "running"
                : entry.failed
                  ? "failed"
                  : "done",
            }).title,
            tone: entry.running
              ? "running"
              : entry.failed
                ? "failed"
                : "success",
            providerToolId: "exec_command",
          }))
        : undefined,
  };
}

function explorationEntryLine(entry: ExplorationEntry) {
  const action = collapsedToolLabel({
    kind: "command",
    command: entry.event.command,
    toolName: "exec_command",
    status: entry.running ? "running" : entry.failed ? "failed" : "done",
  }).title;
  const suffix = entry.running ? " (running)" : entry.failed ? " (failed)" : "";
  return `${action}${suffix}`;
}

function extractDisplayMessage(value: string): {
  body: string;
  attachments: DisplayAttachment[];
} {
  let body = cleanStructuredMessageText(value);
  const attachments: DisplayAttachment[] = [];

  const tagMatch = ATTACHMENT_TAG_RE.exec(body);
  if (tagMatch) {
    attachments.push(...attachmentsFromTag(tagMatch[1]));
    body = cleanStructuredMessageText(body.replace(tagMatch[0], ""));
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
    body: cleanStructuredMessageText(keep.join("\n")),
    attachments,
  };
}

function cleanStructuredMessageText(value: string) {
  return finishDisplayText(stripTerminalControlSequences(value));
}

function cleanTerminalDisplayText(value: string) {
  const withoutSpinnerPrefixes = stripTerminalControlSequences(value)
    .split("\n")
    .map(stripProgressSpinnerPrefix)
    .join("\n");
  return finishDisplayText(withoutSpinnerPrefixes);
}

function finishDisplayText(value: string) {
  return value
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

function stripTerminalControlSequences(value: string) {
  return value
    .replace(/\r\n/g, "\n")
    .replace(/\r/g, "\n")
    .replace(/\u001B\[[0-?]*[ -/]*K\u001B\[[0-?]*[ -/]*G/g, "\n")
    .replace(/\u001B\[[0-?]*[ -/]*G/g, "\n")
    .replace(
      /\u001B(?:\][^\u0007]*(?:\u0007|\u001B\\)|\[[0-?]*[ -/]*[@-~]|[@-Z\\-_])/g,
      "",
    )
    .replace(/\[2K\[1G/g, "\n")
    .replace(/\[(?:\?\d+[hl]|\d+(?:;\d+)*[mKGH])/g, "")
    .replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/g, "");
}

function stripProgressSpinnerPrefix(line: string) {
  return line.replace(
    /^[ \t]*[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏◐◓◑◒]+[ \t]*(?=\S)/,
    "",
  );
}

function patchSummaryFromEvent(event: CodexConversationEvent): PatchSummary {
  const structuredFiles = (event.file_changes ?? []).map((change) => ({
    path: change.path,
    movePath: change.move_path,
    operation: change.operation,
    added: change.additions,
    removed: change.deletions,
  }));
  const parsed = parseApplyPatchSummary(event.body || "");
  const fallbackFiles =
    structuredFiles.length > 0
      ? structuredFiles
      : parsed.files.length > 0
        ? parsed.files
        : (event.files ?? []).map((path) => ({
            path,
            operation: "update" as PatchOperation,
          }));
  const files = fallbackFiles.sort((left, right) =>
    left.path.localeCompare(right.path),
  );
  const { totalAdded, totalRemoved } = knownPatchTotals(files);
  const title = patchSummaryTitle(files, totalAdded, totalRemoved);
  return { title, files, totalAdded, totalRemoved };
}

function parseApplyPatchSummary(patch: string): PatchSummary {
  const normalized = patch.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  const complete = normalized
    .split("\n")
    .some((line) => line.trimEnd() === "*** End Patch");
  const files: PatchFileSummary[] = [];
  let current: PatchFileSummary | null = null;

  const finishCurrent = () => {
    if (!current) {
      return;
    }
    if (!complete || current.operation === "delete") {
      current.added = undefined;
      current.removed = undefined;
    }
    files.push(current);
    current = null;
  };

  for (const rawLine of normalized.split("\n")) {
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
      current.added = (current.added ?? 0) + 1;
    } else if (line.startsWith("-")) {
      current.removed = (current.removed ?? 0) + 1;
    }
  }
  finishCurrent();

  const { totalAdded, totalRemoved } = knownPatchTotals(files);
  return {
    title: patchSummaryTitle(files, totalAdded, totalRemoved),
    files,
    totalAdded,
    totalRemoved,
  };
}

function patchSummaryTitle(
  files: PatchFileSummary[],
  totalAdded?: number,
  totalRemoved?: number,
) {
  if (files.length === 0) {
    return "Edited files";
  }
  if (files.length === 1) {
    const file = files[0];
    const verb =
      file.operation === "add"
        ? "Added"
        : file.operation === "delete"
          ? "Deleted"
          : "Edited";
    const target = safePatchDisplayPath(file) || "1 file";
    return `${verb} ${target}${lineCountSummary(file.added, file.removed)}`;
  }

  let title = `Edited ${files.length} files`;
  const commonDirectory = commonPatchDirectory(files);
  if (commonDirectory) {
    title += ` in ${commonDirectory}`;
  } else {
    const firstTarget = files
      .map(safePatchDisplayPath)
      .find((path): path is string => Boolean(path));
    if (firstTarget) {
      title += ` · ${firstTarget} + ${files.length - 1} more`;
    }
  }
  return title + lineCountSummary(totalAdded, totalRemoved);
}

export function patchDisplayPath(file: PatchFileSummary) {
  return file.movePath ? `${file.path} -> ${file.movePath}` : file.path;
}

function knownPatchTotals(files: PatchFileSummary[]): {
  totalAdded?: number;
  totalRemoved?: number;
} {
  if (
    files.length === 0 ||
    files.some((file) => file.added == null || file.removed == null)
  ) {
    return {};
  }
  return {
    totalAdded: files.reduce((sum, file) => sum + file.added!, 0),
    totalRemoved: files.reduce((sum, file) => sum + file.removed!, 0),
  };
}

function safePatchDisplayPath(file: PatchFileSummary): string | undefined {
  const path = patchDisplayPath(file);
  return isUnsafeCollapsedDetail(path) ? undefined : path;
}

function commonPatchDirectory(files: PatchFileSummary[]): string | undefined {
  const directories = files.map((file) => {
    const path = (file.movePath || file.path).replace(/\\/g, "/");
    const separator = path.lastIndexOf("/");
    return separator > 0 ? path.slice(0, separator) : "";
  });
  if (directories.some((directory) => !directory)) {
    return undefined;
  }
  const segments = directories.map((directory) => directory.split("/"));
  const common = [...segments[0]];
  for (const parts of segments.slice(1)) {
    while (
      common.length > 0 &&
      common.some((segment, index) => segment !== parts[index])
    ) {
      common.pop();
    }
  }
  const directory = common.join("/");
  if (!directory || directory === "/" || isUnsafeCollapsedDetail(directory)) {
    return undefined;
  }
  return directory;
}

function lineCountSummary(added?: number, removed?: number) {
  return added == null || removed == null ? "" : ` (+${added} -${removed})`;
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
  if (normalized.endsWith(".write_stdin")) {
    return isWaitSessionPoll("write_stdin", input);
  }
  return isWaitSessionPoll(normalized, input);
}

export function isEventRunning(event: CodexConversationEvent) {
  return event.partial === true || event.status === "running";
}

function imagePathFromTool(event: CodexConversationEvent) {
  const nested = parseExecWrapperCalls(event.input);
  if (nested.length === 1 && nested[0].name === "view_image") {
    const path =
      typeof nested[0].object?.path === "string"
        ? nested[0].object.path
        : typeof nested[0].object?.image_url === "string"
          ? nested[0].object.image_url
          : "";
    if (path && !previewableImageUri(path) && looksLikeImagePath(path)) {
      return path;
    }
  }
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
  const normalized = cleanTerminalDisplayText(command);
  const firstLine =
    normalized
      .split("\n")
      .find((line) => line.trim())
      ?.trim() || "";
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

  if (
    executable === "ls" ||
    (executable === "find" && !/\s-name\s|\s-iname\s|\s-type\s+f/.test(lower))
  ) {
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

  if (
    ["rg", "grep", "ag", "ack"].includes(executable) ||
    executable === "find"
  ) {
    const query = searchQuery(tokens, executable);
    const target = searchTarget(tokens, executable);
    const detail = [query ? truncateRunes(query, 36) : "", target]
      .filter(Boolean)
      .join(" in ");
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

  if (
    /\b(go test|bun test|npm test|pnpm test|yarn test|jest|vitest|pytest)\b/.test(
      lower,
    )
  ) {
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

  if (
    /\b(tsc|lint|typecheck|doctor|gradlew|xcodebuild|build|assemble)\b/.test(
      lower,
    )
  ) {
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

function commandOutputBodyKind(
  command: string,
  output: string,
): ZenActivityTimelineItem["bodyKind"] {
  const summary = (commandSummary(command) || "").toLowerCase();
  if (/\bgit\s+diff\b/.test(summary) && /\s\|\s+\d+\s+[+-]+/.test(output)) {
    return "diff-stat";
  }
  return "terminal";
}

function toolOutputBodyKind(
  event: CodexConversationEvent,
  output: string,
): ZenActivityTimelineItem["bodyKind"] {
  const name = (event.tool_name || event.title || "")
    .trim()
    .replace(/^functions\./, "");
  if (name === "exec_command" || name === "write_stdin") {
    return commandOutputBodyKind(event.command || "", output);
  }
  return output ? "terminal" : undefined;
}

function webSearchEventDetail(event: CodexConversationEvent) {
  const body = cleanTerminalDisplayText(event.body || "");
  if (body) {
    return body;
  }
  const action = parseToolPayload(event.input);
  if (!isRecord(action)) {
    return undefined;
  }
  return webSearchActionDetail(action) || undefined;
}

function webSearchActivityTitle(
  action: unknown,
  running: boolean,
  failed: boolean,
) {
  if (running) {
    return "Searching the web";
  }
  if (failed) {
    return "Search failed";
  }
  if (!isRecord(action)) {
    return "Searched the web";
  }
  switch (stringField(action, "type")) {
    case "open_page":
      return "Opened page";
    case "find_in_page":
      return "Searched page";
    case "search":
      return "Searched the web";
    default:
      return "Web search";
  }
}

function webSearchActivityBody(event: CodexConversationEvent) {
  const input = cleanTerminalDisplayText(event.input || "");
  if (input) {
    return input;
  }
  const body = cleanTerminalDisplayText(event.body || "");
  return body || undefined;
}

function webSearchActionDetail(action: Record<string, unknown>) {
  const type = stringField(action, "type");
  if (type === "search") {
    const query = stringField(action, "query");
    if (query) {
      return query;
    }
    const firstQuery = firstString(action.queries);
    if (!firstQuery) {
      return "";
    }
    return Array.isArray(action.queries) && action.queries.length > 1
      ? `${firstQuery} ...`
      : firstQuery;
  }
  if (type === "open_page") {
    return stringField(action, "url");
  }
  if (type === "find_in_page") {
    const pattern = stringField(action, "pattern");
    const url = stringField(action, "url");
    if (pattern && url) {
      return `'${pattern}' in ${url}`;
    }
    if (pattern) {
      return `'${pattern}'`;
    }
    return url;
  }
  return (
    stringField(action, "query") ||
    stringField(action, "url") ||
    stringField(action, "pattern")
  );
}

function statusActivityTitle(status?: string) {
  switch ((status || "").trim()) {
    case "failed":
    case "error":
      return "Agent error";
    case "running":
      return "Agent status";
    case "warning":
      return "Agent warning";
    default:
      return "Agent status";
  }
}

function statusActivityTone(status?: string): ZenActivityTimelineItem["tone"] {
  switch ((status || "").trim()) {
    case "failed":
    case "error":
      return "failed";
    case "running":
      return "running";
    default:
      return "neutral";
  }
}

function statusActivityIcon(status?: string): TimelineIconName {
  switch ((status || "").trim()) {
    case "failed":
    case "error":
      return "alert-circle-outline";
    case "warning":
      return "warning-outline";
    case "running":
      return "sync-outline";
    default:
      return "information-circle-outline";
  }
}

function statusActivityDetail(body: string) {
  const clean = cleanTerminalDisplayText(body);
  if (!clean) {
    return undefined;
  }
  const firstLine =
    clean
      .split("\n")
      .find((line) => line.trim())
      ?.trim() || clean;
  return truncateRunes(firstLine, 140);
}

function statusActivityBodyKind(
  event: CodexConversationEvent,
  body: string,
): ZenActivityTimelineItem["bodyKind"] {
  if (!body) {
    return undefined;
  }
  if (event.source === "terminal_snapshot") {
    return "terminal";
  }
  return statusActivityTone(event.status) === "failed" ? "terminal" : undefined;
}

function commandSummary(command: string) {
  command = cleanTerminalDisplayText(command);
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
    const commandIndex = tokens.findIndex(
      (token) => token === "-c" || token === "-lc",
    );
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
    return (
      positional.find(
        (token) => !/^\d*,?\d*p$/.test(token) && !/^s[|/]/.test(token),
      ) || positional[positional.length - 1]
    );
  }
  if (executable === "find") {
    return positional[0];
  }
  return positional[positional.length - 1];
}

function commandPositionals(tokens: string[], executable: string) {
  const start = tokens.findIndex(
    (token) => basename(token).toLowerCase() === executable,
  );
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
        ].includes(token) &&
        relevant[index + 1] &&
        !relevant[index + 1].startsWith("-");
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
    const nameIndex = tokens.findIndex(
      (token) => token === "-name" || token === "-iname",
    );
    return nameIndex >= 0
      ? tokens[nameIndex + 1] || ""
      : positionals.slice(1).join(" ");
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

function isFailureLikeStatus(status?: string): boolean {
  switch ((status || "").trim().toLowerCase()) {
    case "failed":
    case "blocked":
    case "error":
      return true;
    default:
      return false;
  }
}

function isCommandFailed(
  event: CodexConversationEvent,
  presentation: CommandPresentation,
) {
  if (isFailureLikeStatus(event.status)) {
    return true;
  }
  if ((event.exit_code ?? 0) !== 0) {
    if (
      presentation.kind === "search" &&
      event.exit_code === 1 &&
      !cleanToolOutput(event.body || "")
    ) {
      return false;
    }
    return true;
  }
  return false;
}

function cleanToolOutput(value: string) {
  value = cleanTerminalDisplayText(value);
  if (!value) {
    return "";
  }
  const lines = value.split("\n");
  const outputLine = lines.findIndex((line) => line.trim() === "Output:");
  const bodyLines = outputLine >= 0 ? lines.slice(outputLine + 1) : lines;
  return cleanTerminalDisplayText(
    bodyLines.filter((line) => !isToolMetadataLine(line)).join("\n"),
  );
}

function formatOutputPreview(
  value: string,
  options: OutputPreviewOptions,
): OutputPreview {
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
    return compact.replace(/":/g, '": ').replace(/,"/g, ', "');
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
    text: cleanTerminalDisplayText(
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
    text: cleanTerminalDisplayText(
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
  let name = (event.tool_name || event.title || "tool").trim() || "tool";
  name = name.replace(/^functions\./, "");
  let input = parseToolPayload(event.input);
  let inputObject = isRecord(input) ? input : {};

  if (isExecWrapperToolName(name) || name.startsWith("multi:")) {
    const nested = parseExecWrapperCalls(event.input);
    if (nested.length === 1) {
      name = nested[0].name;
      if (nested[0].object) {
        inputObject = nested[0].object;
      } else if (nested[0].text) {
        inputObject = { path: nested[0].text };
      }
    } else if (nested.length > 1) {
      return {
        icon: "git-network-outline",
      };
    }
  }

  const browserAction = /^browser_/.test(name)
    ? humanizeToolName(name.replace(/^browser_/, ""))
    : "";

  if (name === "view_image") {
    const path =
      stringField(inputObject, "path") || stringField(inputObject, "image_url");
    const previewUri = previewableImageUri(path);
    return {
      icon: "image-outline",
      localImagePath: path && !previewUri ? path : undefined,
    };
  }

  if (name === "write_stdin") {
    const chars = stringField(inputObject, "chars");
    return {
      icon:
        chars === ""
          ? "sync-outline"
          : chars === "\u0003"
            ? "stop-circle-outline"
            : "return-down-forward-outline",
    };
  }

  if (browserAction) {
    const browserFile =
      stringField(inputObject, "filename") || firstString(inputObject.paths);
    const browserPreviewUri = looksLikeImagePath(browserFile)
      ? previewableImageUri(browserFile)
      : undefined;
    return {
      icon: browserToolIcon(name),
      localImagePath:
        browserFile && !browserPreviewUri && looksLikeImagePath(browserFile)
          ? browserFile
          : undefined,
    };
  }

  if (name.includes("query_docs") || name.includes("resolve_library_id")) {
    return {
      icon: "library-outline",
    };
  }

  if (name.includes("search_query") || name === "web.run") {
    return {
      icon: "search-outline",
    };
  }

  if (name.includes("multi_tool_use.parallel") || name.startsWith("multi:")) {
    return {
      icon: "git-network-outline",
    };
  }

  if (
    name.includes("spawn_agent") ||
    name.includes("send_input") ||
    name.includes("wait_agent")
  ) {
    return {
      icon: "git-network-outline",
    };
  }

  const semantic = primarySemanticAction({
    toolName: name,
    input: event.input,
    command: event.command,
    status: event.status,
  });
  return {
    icon: semanticKindIcon(semantic.kind),
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

function firstString(value: unknown): string {
  return Array.isArray(value) && typeof value[0] === "string" ? value[0] : "";
}

function humanizeToolName(value: string): string {
  return (
    value
      .replace(/^mcp__/, "")
      .replace(/^functions\./, "")
      .replace(/__/g, " ")
      .replace(/_/g, " ")
      .replace(/\s+/g, " ")
      .trim()
      .replace(/\b\w/g, (letter) => letter.toUpperCase()) || "Tool"
  );
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

function safeCollapsedDetail(value?: string): string | undefined {
  if (!value || isUnsafeCollapsedDetail(value)) {
    return undefined;
  }
  return value;
}

function semanticChildren(
  parentId: string,
  children?: SemanticAction[],
): ZenActivityChild[] | undefined {
  if (!children?.length) {
    return undefined;
  }
  return children.map((child, index) => ({
    id: `${parentId}:action:${index}`,
    title: child.label,
    tone:
      child.status === "running"
        ? "running"
        : child.status === "failed" || child.status === "blocked"
          ? "failed"
          : "success",
    providerToolId: child.providerToolId,
  }));
}

function semanticActivityIcon(
  title: string,
  fallback: TimelineIconName,
  running: boolean,
  failed: boolean,
): TimelineIconName {
  if (running) {
    return "time-outline";
  }
  if (failed) {
    return "alert-circle-outline";
  }
  const lower = title.toLowerCase();
  if (lower.includes("search")) {
    return "search-outline";
  }
  if (lower.includes("read")) {
    return "document-text-outline";
  }
  if (lower.includes("updated files") || lower.includes("updating files")) {
    return "git-compare-outline";
  }
  if (lower.includes("plan")) {
    return "map-outline";
  }
  if (lower.includes("image")) {
    return "image-outline";
  }
  if (lower.includes("test")) {
    return "checkmark-done-outline";
  }
  if (lower.includes("wait")) {
    return "time-outline";
  }
  if (lower.includes("command")) {
    return "terminal-outline";
  }
  return fallback;
}

function semanticKindIcon(kind: SemanticActionKind): TimelineIconName {
  switch (kind) {
    case "read_files":
      return "document-text-outline";
    case "search_code":
      return "search-outline";
    case "run_command":
      return "terminal-outline";
    case "update_files":
      return "git-compare-outline";
    case "update_plan":
      return "map-outline";
    case "view_image":
      return "image-outline";
    case "test_app":
      return "checkmark-done-outline";
    case "wait":
      return "time-outline";
    default:
      return "cube-outline";
  }
}
