import { useEffect, useState } from "react";
import type { ConnectionState } from "../../store/agents";
import { wsClient, type CodexSlashCommand } from "../../services/websocket";
import { slashCommandTitle } from "./codexSlashCommandPresentation";

export type SlashCommandRequest = {
  command: CodexSlashCommand;
  rawText: string;
  known: boolean;
};

type LocalSlashCommandCapability = Pick<
  CodexSlashCommand,
  | "category"
  | "execution"
  | "input"
  | "output"
  | "interactive"
  | "chat_supported"
  | "terminal_supported"
>;

const FALLBACK_SLASH_COMMANDS = [
  ["model", "choose what model and reasoning effort to use"],
  ["fast", "1.5x speed, increased usage"],
  ["ide", "include current selection, open files, and other context from your IDE"],
  ["permissions", "choose what Codex is allowed to do"],
  ["keymap", "remap TUI shortcuts"],
  ["setup-default-sandbox", "set up elevated agent sandbox"],
  ["sandbox-add-read-dir", "let sandbox read a directory: /sandbox-add-read-dir <absolute_path>"],
  ["vim", "toggle Vim mode for the composer"],
  ["experimental", "toggle experimental features"],
  ["approve", "approve one retry of a recent auto-review denial"],
  ["memories", "configure memory use and generation"],
  ["skills", "use skills to improve how Codex performs specific tasks"],
  ["hooks", "view and manage lifecycle hooks"],
  ["review", "review my current changes and find issues"],
  ["rename", "rename the current thread"],
  ["new", "start a new chat during a conversation"],
  ["resume", "resume a saved chat"],
  ["fork", "fork the current chat"],
  ["init", "create an AGENTS.md file with instructions for Codex"],
  ["compact", "summarize conversation to prevent hitting the context limit"],
  ["plan", "switch to Plan mode"],
  ["goal", "set or view the goal for a long-running task"],
  ["side", "start a side conversation in an ephemeral fork"],
  ["copy", "copy last response as markdown"],
  ["raw", "toggle raw scrollback mode for copy-friendly terminal selection"],
  ["diff", "show git diff (including untracked files)"],
  ["mention", "mention a file"],
  ["status", "show current session configuration and token usage"],
  ["debug-config", "show config layers and requirement sources for debugging"],
  ["title", "configure which items appear in the terminal title"],
  ["statusline", "configure which items appear in the status line"],
  ["theme", "choose a syntax highlighting theme"],
  ["pets", "choose or hide the terminal pet"],
  ["mcp", "list configured MCP tools; use /mcp verbose for details"],
  ["apps", "manage apps"],
  ["plugins", "browse plugins"],
  ["logout", "log out of Codex"],
  ["quit", "exit Codex"],
  ["exit", "exit Codex"],
  ["feedback", "send logs to maintainers"],
  ["rollout", "print the rollout file path"],
  ["ps", "list background terminals"],
  ["stop", "stop all background terminals"],
  ["clear", "clear the terminal and start a new chat"],
  ["personality", "choose a communication style for Codex"],
  ["realtime", "toggle realtime voice mode (experimental)"],
  ["settings", "configure realtime microphone/speaker"],
  ["test-approval", "test approval request"],
  ["agent", "switch the active agent thread"],
  ["subagents", "switch the active agent thread"],
  ["btw", "start a side conversation in an ephemeral fork"],
  ["debug-m-drop", "DO NOT USE"],
  ["debug-m-update", "DO NOT USE"],
].map(([name, description]) => ({
  value: `/${name}`,
  name,
  title: slashCommandTitle(name),
  description,
  source: "fallback",
  ...fallbackSlashCommandCapability(name),
})) satisfies CodexSlashCommand[];

const slashCommandCache = new Map<string, CodexSlashCommand[]>();

export function useCodexSlashCommands({
  serverId,
  connectionState,
  screenFocused,
}: {
  serverId: string;
  connectionState: ConnectionState;
  screenFocused: boolean;
}) {
  const [slashCommands, setSlashCommands] = useState<CodexSlashCommand[]>(
    () => slashCommandCache.get(serverId) ?? FALLBACK_SLASH_COMMANDS,
  );

  useEffect(() => {
    const cachedCommands = slashCommandCache.get(serverId);
    setSlashCommands(
      cachedCommands && cachedCommands.length > 0
        ? cachedCommands
        : FALLBACK_SLASH_COMMANDS,
    );

    if (!screenFocused || connectionState !== "connected") {
      return;
    }

    let cancelled = false;
    void wsClient
      .getCodexSlashCommands(serverId)
      .then((snapshot) => {
        if (cancelled) {
          return;
        }
        const nextCommands = normalizeSlashCommands(snapshot.commands);
        if (nextCommands.length === 0) {
          return;
        }
        slashCommandCache.set(serverId, nextCommands);
        setSlashCommands(nextCommands);
      })
      .catch(() => {
        // The fallback list keeps slash commands usable on older daemons.
      });

    return () => {
      cancelled = true;
    };
  }, [connectionState, screenFocused, serverId]);

  return slashCommands;
}

export function slashCommandRequestFromDraft(
  draft: string,
  commands: CodexSlashCommand[],
): SlashCommandRequest | null {
  const trimmedStart = draft.trimStart();
  if (!trimmedStart.startsWith("/")) {
    return null;
  }
  const firstLine = trimmedStart.split(/\r?\n/, 1)[0] || "";
  const match = /^\/([a-z][a-z0-9-]*)(?:\s|$)/.exec(firstLine);
  if (!match) {
    return null;
  }
  const name = match[1];
  const command = commands.find((candidate) => candidate.name === name);
  if (command) {
    return { command, rawText: trimmedStart, known: true };
  }
  return {
    command: {
      value: `/${name}`,
      name,
      title: slashCommandTitle(name),
      description: "Unknown Codex slash command",
      source: "draft",
      ...fallbackSlashCommandCapability(name),
    },
    rawText: trimmedStart,
    known: false,
  };
}

export function requiresSlashCommandArgs(command: CodexSlashCommand) {
  return (
    command.input.kind === "inline-args" ||
    command.input.kind === "freeform" ||
    command.input.kind === "form"
  );
}

export function slashCommandHasArgs(rawText: string, command: CodexSlashCommand) {
  const args = rawText.trimStart().slice(command.value.length).trim();
  return args.length > 0;
}

export function slashCommandTerminalText(
  command: CodexSlashCommand,
  rawText?: string,
) {
  const text = rawText?.trim();
  if (text?.startsWith(command.value)) {
    return text;
  }
  return command.value;
}

export function slashCommandTerminalMessage(command: CodexSlashCommand) {
  if (command.interactive) {
    return "This command can open Codex prompts, pickers, or terminal-only views. The chat renderer cannot represent that interaction yet.";
  }
  if (command.output.kind === "terminal") {
    return "This command writes terminal-oriented output. Open it in Terminal for correct rendering, or send it anyway as a normal message.";
  }
  return "Zen does not have a native chat renderer for this command yet.";
}

export function filterSlashCommands(
  commands: CodexSlashCommand[],
  commandQuery: string,
) {
  if (!commandQuery.startsWith("/")) {
    return [];
  }
  const query = commandQuery.slice(1).toLowerCase();
  if (!query) {
    return commands;
  }

  return commands
    .map((command, index) => {
      const name = command.name.toLowerCase();
      const value = command.value.toLowerCase();
      const title = command.title.toLowerCase();
      const description = command.description.toLowerCase();
      let score = Number.POSITIVE_INFINITY;
      if (name === query || value === `/${query}`) {
        score = 0;
      } else if (name.startsWith(query) || value.startsWith(`/${query}`)) {
        score = 1;
      } else if (title.startsWith(query)) {
        score = 2;
      } else if (name.includes(query) || value.includes(query)) {
        score = 3;
      } else if (title.includes(query)) {
        score = 4;
      } else if (description.includes(query)) {
        score = 5;
      }
      return { command, index, score };
    })
    .filter((entry) => Number.isFinite(entry.score))
    .sort((a, b) => a.score - b.score || a.index - b.index)
    .map((entry) => entry.command);
}

function normalizeSlashCommands(commands: CodexSlashCommand[]) {
  const seen = new Set<string>();
  const normalized: CodexSlashCommand[] = [];
  for (const command of commands) {
    const name = command.name.trim().replace(/^\//, "");
    const rawValue = command.value.trim();
    const value = rawValue.length > 1 && rawValue.startsWith("/") ? rawValue : `/${name}`;
    if (!name || !value.startsWith("/") || seen.has(value)) {
      continue;
    }
    seen.add(value);
    normalized.push({
      value,
      name,
      title: command.title.trim() || slashCommandTitle(name),
      description: command.description.trim(),
      source: command.source,
      ...normalizeSlashCommandCapability(name, command),
    });
  }
  return normalized.length > 0 ? normalized : FALLBACK_SLASH_COMMANDS;
}

function normalizeSlashCommandCapability(
  name: string,
  command: Partial<CodexSlashCommand>,
): LocalSlashCommandCapability {
  const fallback = fallbackSlashCommandCapability(name);
  const execution =
    typeof command.execution === "string" && command.execution.trim()
      ? command.execution.trim()
      : fallback.execution;
  const terminalSupported =
    typeof command.terminal_supported === "boolean"
      ? command.terminal_supported
      : fallback.terminal_supported;
  return {
    category:
      typeof command.category === "string" && command.category.trim()
        ? command.category.trim()
        : fallback.category,
    execution,
    input: {
      kind:
        command.input?.kind && typeof command.input.kind === "string"
          ? command.input.kind
          : fallback.input.kind,
      placeholder:
        typeof command.input?.placeholder === "string"
          ? command.input.placeholder
          : fallback.input.placeholder,
      picker:
        typeof command.input?.picker === "string"
          ? command.input.picker
          : fallback.input.picker,
    },
    output: {
      kind:
        command.output?.kind && typeof command.output.kind === "string"
          ? command.output.kind
          : fallback.output.kind,
    },
    interactive:
      typeof command.interactive === "boolean"
        ? command.interactive
        : fallback.interactive,
    chat_supported:
      typeof command.chat_supported === "boolean"
        ? command.chat_supported
        : fallback.chat_supported,
    terminal_supported: terminalSupported,
  };
}

function fallbackSlashCommandCapability(name: string): LocalSlashCommandCapability {
  switch (name) {
    case "status":
      return chatSlashCapability("session", "status-card");
    case "diff":
      return chatSlashCapability("tools", "diff");
    case "copy":
      return chatSlashCapability("tools", "none");
    case "debug-m-drop":
    case "debug-m-update":
      return {
        category: "debug",
        execution: "unsupported",
        input: { kind: "none" },
        output: { kind: "terminal" },
        interactive: true,
        chat_supported: false,
        terminal_supported: false,
      };
    case "debug-config":
    case "test-approval":
      return terminalSlashCapability("debug", { kind: "none" }, true);
    default:
      return terminalSlashCapability(
        slashCommandDefaultCategory(name),
        slashCommandDefaultInput(name),
        slashCommandDefaultsToInteractive(name),
      );
  }
}

function chatSlashCapability(
  category: string,
  outputKind: CodexSlashCommand["output"]["kind"],
): LocalSlashCommandCapability {
  return {
    category,
    execution: "chat-native",
    input: { kind: "none" },
    output: { kind: outputKind },
    interactive: false,
    chat_supported: true,
    terminal_supported: true,
  };
}

function terminalSlashCapability(
  category: string,
  input: CodexSlashCommand["input"],
  interactive: boolean,
): LocalSlashCommandCapability {
  return {
    category,
    execution: "terminal-required",
    input,
    output: { kind: input.kind === "picker" ? "management-screen" : "terminal" },
    interactive,
    chat_supported: false,
    terminal_supported: true,
  };
}

function slashCommandDefaultCategory(name: string) {
  switch (name) {
    case "model":
    case "fast":
    case "ide":
    case "permissions":
    case "keymap":
    case "setup-default-sandbox":
    case "sandbox-add-read-dir":
    case "vim":
    case "experimental":
    case "title":
    case "statusline":
    case "theme":
    case "pets":
    case "personality":
    case "realtime":
    case "settings":
      return "settings";
    case "resume":
    case "fork":
    case "side":
    case "agent":
    case "subagents":
    case "btw":
      return "navigation";
    case "memories":
    case "skills":
    case "hooks":
    case "mcp":
    case "apps":
    case "plugins":
    case "feedback":
      return "management";
    case "logout":
    case "quit":
    case "exit":
      return "danger";
    case "review":
    case "init":
    case "mention":
    case "raw":
    case "rollout":
    case "ps":
    case "stop":
      return "tools";
    case "approve":
    case "rename":
    case "new":
    case "compact":
    case "plan":
    case "goal":
    case "clear":
      return "session";
    default:
      return name.startsWith("debug-") ? "debug" : "unknown";
  }
}

function slashCommandDefaultInput(name: string): CodexSlashCommand["input"] {
  switch (name) {
    case "fast":
      return { kind: "inline-args", placeholder: "optional speed mode" };
    case "model":
    case "permissions":
    case "resume":
    case "fork":
    case "mention":
    case "theme":
    case "pets":
    case "personality":
    case "agent":
    case "subagents":
      return { kind: "picker", picker: name };
    case "sandbox-add-read-dir":
      return { kind: "inline-args", placeholder: "<absolute_path>" };
    case "mcp":
      return { kind: "inline-args", placeholder: "verbose" };
    case "rename":
      return { kind: "freeform", placeholder: "new thread title" };
    case "goal":
      return { kind: "freeform", placeholder: "goal text" };
    case "side":
    case "btw":
      return { kind: "freeform", placeholder: "side conversation prompt" };
    default:
      return { kind: "none" };
  }
}

function slashCommandDefaultsToInteractive(name: string) {
  const input = slashCommandDefaultInput(name);
  if (input.kind === "picker" || input.kind === "form") {
    return true;
  }
  return [
    "model",
    "fast",
    "ide",
    "permissions",
    "keymap",
    "setup-default-sandbox",
    "vim",
    "experimental",
    "memories",
    "skills",
    "hooks",
    "new",
    "resume",
    "fork",
    "side",
    "raw",
    "mention",
    "title",
    "statusline",
    "theme",
    "pets",
    "mcp",
    "apps",
    "plugins",
    "logout",
    "quit",
    "exit",
    "feedback",
    "clear",
    "personality",
    "realtime",
    "settings",
    "test-approval",
    "agent",
    "subagents",
    "btw",
  ].includes(name);
}
