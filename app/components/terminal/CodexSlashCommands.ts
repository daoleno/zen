import { useEffect, useState } from "react";
import type { ConnectionState } from "../../store/agents";
import { chatAgentSupportsSlashCommands } from "../../services/chatComposerPresentation";
import type { AgentKind } from "../../services/agentPresentation";
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

type LocalSlashCommandSpec = {
  name: string;
  description: string;
  title?: string;
  source?: string;
} & LocalSlashCommandCapability;

type SlashCommandOverride = Partial<
  Omit<CodexSlashCommand, "name" | "value" | "source">
>;

const CHATUI_FALLBACK_SLASH_COMMAND_SPECS = [
  fallbackSpec({
    name: "review",
    category: "tools",
    execution: "terminal-required",
    input: requiredFreeformInput("review instructions"),
    outputKind: "terminal",
    description: "review my current changes and find issues",
  }),
  fallbackSpec({
    name: "goal",
    category: "session",
    execution: "terminal-required",
    input: optionalFreeformInput("optional goal text"),
    outputKind: "terminal",
    description: "set or view the goal for a long-running task",
  }),
  fallbackSpec({
    name: "rename",
    category: "session",
    execution: "terminal-required",
    input: requiredFreeformInput("new thread title"),
    outputKind: "terminal",
    description: "rename the current thread",
  }),
  fallbackSpec({
    name: "new",
    category: "session",
    execution: "native",
    input: inputNone(),
    outputKind: "none",
    description: "start a new chat during a conversation",
  }),
  fallbackSpec({
    name: "init",
    category: "tools",
    execution: "terminal-required",
    input: inputNone(),
    outputKind: "terminal",
    description: "create an AGENTS.md file with instructions for Codex",
    chatSupported: false,
  }),
  fallbackSpec({
    name: "skills",
    category: "management",
    execution: "native",
    input: inputNone(),
    outputKind: "management-screen",
    description: "use skills to improve how Codex performs specific tasks",
    interactive: true,
  }),
] satisfies LocalSlashCommandSpec[];

const CHATUI_SLASH_COMMAND_OVERRIDES: Record<string, SlashCommandOverride> = {
  new: {
    execution: "native",
    input: inputNone(),
    output: { kind: "none" },
    interactive: false,
    chat_supported: true,
    terminal_supported: true,
  },
  clear: {
    execution: "native",
    input: inputNone(),
    output: { kind: "none" },
    interactive: false,
    chat_supported: false,
    terminal_supported: true,
  },
  skills: {
    execution: "native",
    input: inputNone(),
    output: { kind: "management-screen" },
    interactive: true,
    chat_supported: true,
    terminal_supported: true,
  },
  init: {
    execution: "terminal-required",
    input: inputNone(),
    output: { kind: "terminal" },
    interactive: false,
    chat_supported: false,
    terminal_supported: true,
  },
  rename: {
    execution: "terminal-required",
    input: requiredFreeformInput("new thread title"),
    output: { kind: "terminal" },
    interactive: false,
    chat_supported: true,
    terminal_supported: true,
  },
  review: {
    execution: "terminal-required",
    input: requiredFreeformInput("review instructions"),
    output: { kind: "terminal" },
    interactive: false,
    chat_supported: true,
    terminal_supported: true,
  },
  goal: {
    execution: "terminal-required",
    input: optionalFreeformInput("optional goal text"),
    output: { kind: "terminal" },
    interactive: false,
    chat_supported: true,
    terminal_supported: true,
  },
  stop: {
    chat_supported: false,
  },
};

const HIDDEN_CHATUI_SLASH_COMMAND_NAMES = new Set([
  "compact",
  "copy",
  "diff",
  "mcp",
  "plan",
  "ps",
  "rollout",
  "status",
  "theme",
]);

const CHATUI_SLASH_COMMANDS = buildChatuiSlashCommands();

export function useCodexSlashCommands({
  serverId,
  connectionState,
  screenFocused,
  agentKind = "codex",
}: {
  serverId: string;
  connectionState: ConnectionState;
  screenFocused: boolean;
  agentKind?: AgentKind;
}) {
  const slashCommandsEnabled = chatAgentSupportsSlashCommands(agentKind);
  const [slashCommands, setSlashCommands] = useState<CodexSlashCommand[]>(
    () => slashCommandsEnabled ? CHATUI_SLASH_COMMANDS : [],
  );

  useEffect(() => {
    setSlashCommands(slashCommandsEnabled ? CHATUI_SLASH_COMMANDS : []);

    if (!slashCommandsEnabled) {
      return;
    }

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
        const nextCommands = snapshot.commands.length > 0
          ? buildChatuiSlashCommands(snapshot.commands)
          : CHATUI_SLASH_COMMANDS;
        setSlashCommands(nextCommands);
      })
      .catch(() => {
        if (!cancelled) {
          setSlashCommands(CHATUI_SLASH_COMMANDS);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [connectionState, screenFocused, serverId, slashCommandsEnabled]);

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

export function slashCommandAcceptsArgs(command: CodexSlashCommand) {
  return (
    command.input.kind === "inline-args" ||
    command.input.kind === "freeform" ||
    command.input.kind === "form"
  );
}

export function requiresSlashCommandArgs(command: CodexSlashCommand) {
  if (!slashCommandAcceptsArgs(command)) {
    return false;
  }
  if (typeof command.input.required === "boolean") {
    return command.input.required;
  }
  return false;
}

export function slashCommandHasArgs(rawText: string, command: CodexSlashCommand) {
  const args = rawText.trimStart().slice(command.value.length).trim();
  return args.length > 0;
}

export function filterSlashCommands(
  commands: CodexSlashCommand[],
  commandQuery: string,
) {
  if (!commandQuery.startsWith("/")) {
    return [];
  }
  const query = commandQuery.slice(1).toLowerCase();
  const visibleCommands = commands.filter(isVisibleChatSlashCommand);
  if (!query) {
    return visibleCommands;
  }

  return visibleCommands
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

function buildChatuiSlashCommands(discoveredCommands: CodexSlashCommand[] = []) {
  const sourceCommands =
    discoveredCommands.length > 0
      ? discoveredCommands
      : CHATUI_FALLBACK_SLASH_COMMAND_SPECS.map(commandFromFallbackSpec);
  const commands: CodexSlashCommand[] = [];
  const seen = new Set<string>();
  for (const command of sourceCommands) {
    const normalized = normalizeSlashCommand(command);
    if (!normalized || seen.has(normalized.name) || isHiddenSlashCommand(normalized.name)) {
      continue;
    }
    seen.add(normalized.name);
    commands.push(normalized);
  }
  return commands;
}

function normalizeSlashCommand(command: CodexSlashCommand) {
  const name = command.name.trim().replace(/^\//, "");
  if (!name) {
    return null;
  }
  const override = CHATUI_SLASH_COMMAND_OVERRIDES[name] ?? {};
  return {
    value: `/${name}`,
    name,
    title: override.title?.trim() || command.title?.trim() || slashCommandTitle(name),
    description: override.description?.trim() || command.description?.trim() || "Codex slash command",
    source: command.source || "codex",
    category: override.category || command.category || "unknown",
    execution: override.execution || command.execution || "terminal-required",
    input: mergeSlashCommandInput(command.input, override.input),
    output: mergeSlashCommandOutput(command.output, override.output),
    interactive: override.interactive ?? command.interactive,
    chat_supported: override.chat_supported ?? command.chat_supported,
    terminal_supported:
      override.terminal_supported ?? command.terminal_supported ?? command.execution !== "unsupported",
  } satisfies CodexSlashCommand;
}

function mergeSlashCommandInput(
  baseInput: CodexSlashCommand["input"],
  overrideInput?: CodexSlashCommand["input"],
) {
  return {
    kind: overrideInput?.kind || baseInput?.kind || "none",
    placeholder: overrideInput?.placeholder ?? baseInput?.placeholder,
    picker: overrideInput?.picker ?? baseInput?.picker,
    required: overrideInput?.required ?? baseInput?.required,
  };
}

function mergeSlashCommandOutput(
  baseOutput: CodexSlashCommand["output"],
  overrideOutput?: CodexSlashCommand["output"],
) {
  return {
    kind: overrideOutput?.kind || baseOutput?.kind || "terminal",
  };
}

function commandFromFallbackSpec(spec: LocalSlashCommandSpec): CodexSlashCommand {
  return {
    value: `/${spec.name}`,
    name: spec.name,
    title: spec.title || slashCommandTitle(spec.name),
    description: spec.description,
    source: spec.source || "chatui-fallback",
    category: spec.category,
    execution: spec.execution,
    input: spec.input,
    output: spec.output,
    interactive: spec.interactive,
    chat_supported: spec.chat_supported,
    terminal_supported: spec.terminal_supported,
  };
}

function fallbackSpec({
  name,
  category,
  execution,
  input,
  outputKind,
  description,
  interactive = false,
  chatSupported = true,
}: {
  name: string;
  category: string;
  execution: string;
  input: CodexSlashCommand["input"];
  outputKind: string;
  description: string;
  interactive?: boolean;
  chatSupported?: boolean;
}): LocalSlashCommandSpec {
  return {
    name,
    description,
    category,
    execution,
    input,
    output: { kind: outputKind },
    interactive,
    chat_supported: chatSupported,
    terminal_supported: true,
  };
}

function inputNone(): CodexSlashCommand["input"] {
  return { kind: "none" };
}

function optionalFreeformInput(placeholder: string): CodexSlashCommand["input"] {
  return { kind: "freeform", placeholder, required: false };
}

function requiredFreeformInput(placeholder: string): CodexSlashCommand["input"] {
  return { kind: "freeform", placeholder, required: true };
}

function isVisibleChatSlashCommand(command: CodexSlashCommand) {
  return (
    command.chat_supported &&
    command.execution !== "unsupported" &&
    !isHiddenSlashCommand(command.name)
  );
}

function isHiddenSlashCommand(name: string) {
  const normalized = name.toLowerCase();
  return HIDDEN_CHATUI_SLASH_COMMAND_NAMES.has(normalized) || normalized.startsWith("debug-");
}

function fallbackSlashCommandCapability(name: string): LocalSlashCommandCapability {
  return {
    category: name.startsWith("debug-") ? "debug" : "unknown",
    execution: "unsupported",
    input: { kind: "none" },
    output: { kind: "none" },
    interactive: true,
    chat_supported: false,
    terminal_supported: false,
  };
}
