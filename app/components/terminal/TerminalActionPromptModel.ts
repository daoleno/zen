import { isGrokCommand } from "../../services/agentCommands";

export type TerminalActionPromptOption = {
  id: string;
  label: string;
  description?: string;
  key: string;
  primary?: boolean;
  destructive?: boolean;
  default?: boolean;
};

export type TerminalActionPrompt = {
  id: string;
  title: string;
  detail: string;
  requestLabel?: string;
  requestText?: string;
  defaultOptionId?: string;
  options: TerminalActionPromptOption[];
  /** False when the live surface proves choices but no fail-fast native input contract. */
  actionable?: boolean;
  inputHints?: string;
};

const NUMBERED_OPTION_RE = /^(?:[›>\s]*)?([1-9])\.\s+(.+?)\s*$/;
// Capture-pane may keep bare glyphs or parenthesize them as (○)/(●)/(O).
const GROK_NUMBERED_OPTION_RE =
  /^(?:[›>\s]*)?([1-9])\s*([○●◯◉◎]|\([○●◯◉◎Oo\s]*\))\s+(.+?)\s*$/;
const GROK_FILLED_RADIO_RE = /^[●◉◎]$|^\([●◉◎]\)$/;
const GROK_CHOICE_FOOTER_RE =
  /(?:↑\s*\/\s*↓|↑|↓).{0,24}navigate|\bnavigate\b.{0,40}\benter\b|\benter\s*:\s*submit\b|\benter\s+confirm\b/i;
const ANSI_RE = /\x1b\[[0-9;?]*[ -/]*[@-~]/g;

type CleanPromptLine = {
  text: string;
  selected: boolean;
};

export function buildTerminalActionPrompt({
  status,
  summary,
  lastOutputLines,
  command,
  scopeKey,
}: {
  status?: string;
  summary?: string;
  lastOutputLines?: string[];
  /** Live pane command; Grok radio menus are only projected for Grok. */
  command?: string;
  /** Live process/session identity so a switched generation cannot reuse card state. */
  scopeKey?: string;
}): TerminalActionPrompt | null {
  if (status !== "blocked") {
    return null;
  }
  const lines = cleanTerminalPromptLines(lastOutputLines ?? []);
  if (isGrokCommand(command) && looksLikeGrokChoiceMenu(lines)) {
    return scopePrompt(buildGrokChoicePrompt(lines, summary), scopeKey);
  }
  if (!looksLikeTerminalActionPrompt(lines, summary)) {
    return null;
  }

  const options = extractNumberedOptions(lines);
  if (options.length > 0) {
    const request = extractRequest(lines, summary);
    const defaultOption = options.find((option) => option.default) ?? options[0];
    return scopePrompt(
      {
        id: promptId(request.text, options),
        title: "Waiting for confirmation",
        detail: "Codex needs your decision before it can continue.",
        requestLabel: request.label,
        requestText: request.text,
        defaultOptionId: defaultOption?.id,
        options,
        actionable: true,
      },
      scopeKey,
    );
  }

  const request = extractRequest(lines, summary);
  return scopePrompt(
    {
      id: promptId(request.text, ["enter", "escape"].map((id) => ({ id, label: id, key: id }))),
      title: "Waiting for confirmation",
      detail: "Codex needs your decision before it can continue.",
      requestLabel: request.label,
      requestText: request.text,
      defaultOptionId: "enter",
      options: [
        {
          id: "enter",
          label: "Allow once",
          description: "Confirm this request this time.",
          key: "Enter",
          primary: true,
          default: true,
        },
        {
          id: "escape",
          label: "Deny / tell agent what to do",
          description: "Cancel this request and return control to Codex.",
          key: "Escape",
          destructive: true,
        },
      ],
      actionable: true,
    },
    scopeKey,
  );
}

function scopePrompt(
  prompt: TerminalActionPrompt | null,
  scopeKey?: string,
): TerminalActionPrompt | null {
  if (!prompt) {
    return null;
  }
  const scope = scopeKey?.trim();
  if (!scope) {
    return prompt;
  }
  return {
    ...prompt,
    id: `${scope}|${prompt.id}`,
  };
}

function cleanTerminalPromptLines(lines: string[]) {
  return lines
    .map((line) => {
      const cleaned = line.replace(ANSI_RE, "");
      const selected = /^\s*[›>]\s*/.test(cleaned);
      return {
        text: cleaned.replace(/^\s*[›>]\s*/, "").trim(),
        selected,
      };
    })
    .filter((line) => line.text.length > 0)
    .slice(-24);
}

function looksLikeTerminalActionPrompt(lines: CleanPromptLine[], summary?: string) {
  const text = [...lines.map((line) => line.text), summary ?? ""].join("\n").toLowerCase();
  return (
    text.includes("action required") ||
    text.includes("press enter to confirm") ||
    text.includes("press enter to continue") ||
    text.includes("esc to cancel") ||
    text.includes("don't ask again")
  );
}

function looksLikeGrokChoiceMenu(lines: CleanPromptLine[]) {
  const hasOptions = lines.some((line) => GROK_NUMBERED_OPTION_RE.test(line.text));
  const hasFooter = lines.some((line) => GROK_CHOICE_FOOTER_RE.test(line.text));
  return hasOptions && hasFooter;
}

function buildGrokChoicePrompt(
  lines: CleanPromptLine[],
  summary?: string,
): TerminalActionPrompt | null {
  const options = extractGrokNumberedOptions(lines);
  if (options.length === 0) {
    return null;
  }
  const title = extractGrokChoiceTitle(lines, summary);
  const footer = lines.map((line) => line.text).find((text) => GROK_CHOICE_FOOTER_RE.test(text));
  const selected = options.find((option) => option.default);
  return {
    id: promptId(title, options),
    title: title || "Provider choice required",
    detail: "Grok is waiting for a selection in the live terminal.",
    defaultOptionId: selected?.id,
    options,
    actionable: false,
    inputHints: footer ? truncateRunes(footer.replace(/\s+/g, " ").trim(), 120) : undefined,
  };
}

function extractNumberedOptions(lines: CleanPromptLine[]) {
  const parsed: Array<TerminalActionPromptOption & { selected?: boolean }> = [];
  const seen = new Set<string>();
  for (const line of mergeWrappedOptionLines(lines, NUMBERED_OPTION_RE)) {
    const match = NUMBERED_OPTION_RE.exec(line.text);
    if (!match) {
      continue;
    }
    const number = match[1];
    if (seen.has(number)) {
      continue;
    }
    seen.add(number);
    const text = match[2].trim();
    const presentation = optionPresentation(text);
    parsed.push({
      id: number,
      label: presentation.label,
      description: presentation.description,
      key: optionKey(number, text),
      selected: line.selected,
      destructive: isNegativeOption(text),
    });
  }

  const options = parsed.slice(0, 4);
  const defaultIndex = Math.max(
    0,
    options.findIndex((option) => option.selected),
  );
  return options.map((option, index) => ({
    id: option.id,
    label: option.label,
    description: option.description,
    key: option.key,
    primary: index === defaultIndex,
    destructive: option.destructive,
    default: index === defaultIndex,
  }));
}

function extractGrokNumberedOptions(lines: CleanPromptLine[]) {
  const parsed: Array<TerminalActionPromptOption & { selected?: boolean }> = [];
  const seen = new Set<string>();
  for (const line of mergeWrappedOptionLines(lines, GROK_NUMBERED_OPTION_RE)) {
    const match = GROK_NUMBERED_OPTION_RE.exec(line.text);
    if (!match) {
      continue;
    }
    const number = match[1];
    if (seen.has(number)) {
      continue;
    }
    seen.add(number);
    const marker = match[2];
    const { label, description } = splitGrokOptionText(match[3].trim());
    parsed.push({
      id: number,
      label: truncateRunes(label, 54),
      description: description ? truncateRunes(description, 120) : undefined,
      key: number,
      // Prove selection from caret prefix or a single filled radio glyph — never invent it.
      selected: line.selected || GROK_FILLED_RADIO_RE.test(marker),
    });
  }

  const options = parsed.slice(0, 6);
  // Ambiguous multi-selection markers are treated as unproven rather than guessed.
  const provenSelectedCount = options.filter((option) => option.selected).length;
  const selectionProven = provenSelectedCount === 1;
  return options.map((option) => ({
    id: option.id,
    label: option.label,
    description: option.description,
    key: option.key,
    primary: selectionProven && Boolean(option.selected),
    default: selectionProven && Boolean(option.selected),
  }));
}

function splitGrokOptionText(text: string) {
  const parts = text.split(/\s{2,}/).map((part) => part.trim()).filter(Boolean);
  if (parts.length >= 2) {
    return {
      label: parts[0],
      description: parts.slice(1).join(" "),
    };
  }
  return { label: text, description: undefined };
}

function extractGrokChoiceTitle(lines: CleanPromptLine[], summary?: string) {
  const firstOptionIndex = lines.findIndex((line) => GROK_NUMBERED_OPTION_RE.test(line.text));
  const candidates =
    firstOptionIndex > 0 ? lines.slice(0, firstOptionIndex) : lines;
  for (let index = candidates.length - 1; index >= 0; index -= 1) {
    const text = candidates[index].text.trim();
    if (!text) {
      continue;
    }
    if (GROK_NUMBERED_OPTION_RE.test(text) || GROK_CHOICE_FOOTER_RE.test(text)) {
      continue;
    }
    if (looksLikeTerminalStatusLine(text) || looksLikePromptInstruction(text)) {
      continue;
    }
    if (/^(esc:|tab:|shift\+tab:)/i.test(text) || looksLikeKeybindingChrome(text)) {
      continue;
    }
    return truncateRunes(text.replace(/\s+/g, " "), 80);
  }
  return truncateRunes((summary ?? "").replace(/\s+/g, " ").trim(), 80);
}

function mergeWrappedOptionLines(lines: CleanPromptLine[], optionRe: RegExp) {
  const merged: CleanPromptLine[] = [];
  for (const line of lines) {
    if (optionRe.test(line.text)) {
      merged.push(line);
      continue;
    }
    if (
      GROK_CHOICE_FOOTER_RE.test(line.text) ||
      looksLikePromptInstruction(line.text) ||
      looksLikeTerminalStatusLine(line.text) ||
      looksLikeKeybindingChrome(line.text) ||
      GROK_NUMBERED_OPTION_RE.test(line.text) ||
      NUMBERED_OPTION_RE.test(line.text)
    ) {
      continue;
    }
    if (merged.length > 0) {
      merged[merged.length - 1] = {
        ...merged[merged.length - 1],
        text: `${merged[merged.length - 1].text} ${line.text}`.replace(/\s+/g, " ").trim(),
      };
    }
  }
  return merged;
}

function looksLikeKeybindingChrome(line: string) {
  return /^(esc:|tab:|shift\+tab:)/i.test(line.trim()) || /\|\s*(esc|tab|ctrl)\b/i.test(line);
}

function looksLikePromptInstruction(line: string) {
  const lower = line.toLowerCase();
  return (
    lower.includes("press enter") ||
    lower.includes("esc to cancel") ||
    lower.includes("action required")
  );
}

function looksLikeTerminalStatusLine(line: string) {
  return /^\[[^\]]+\]\s+/.test(line);
}

function optionKey(number: string, text: string) {
  const shortcut = /\(([^()]{1,16})\)\s*$/.exec(text)?.[1]?.trim().toLowerCase();
  switch (shortcut) {
    case "esc":
    case "escape":
      return "Escape";
    case "enter":
    case "return":
      return "Enter";
    case "space":
      return "Space";
    default:
      return shortcut && /^[a-z0-9]$/.test(shortcut) ? shortcut : number;
  }
}

function isNegativeOption(text: string) {
  const lower = text.toLowerCase();
  return lower.startsWith("no") || lower.includes("cancel") || lower.includes("reject");
}

function optionPresentation(text: string) {
  const lower = text.toLowerCase();
  const compact = stripShortcutSuffix(text).replace(/\s+/g, " ").trim();
  if (lower.includes("don't ask again") || lower.includes("always allow")) {
    const matching = matchingCommandDescription(text);
    return {
      label: "Always allow matching command",
      description: matching || "Approve this request and similar commands.",
    };
  }
  if (isNegativeOption(text) || lower.includes("tell codex what to do")) {
    return {
      label: "Deny / tell agent what to do",
      description: "Cancel this request and return control to Codex.",
    };
  }
  if (
    lower.startsWith("yes") ||
    lower.startsWith("allow") ||
    lower.includes("proceed") ||
    lower.includes("confirm")
  ) {
    return {
      label: "Allow once",
      description: "Approve this request this time.",
    };
  }
  return {
    label: truncateRunes(compact, 54),
    description: undefined,
  };
}

function matchingCommandDescription(text: string) {
  const backtick = /`([^`]+)`/.exec(text)?.[1]?.trim();
  if (!backtick) {
    return "";
  }
  if (text.toLowerCase().includes("start")) {
    return `Matches commands starting with ${backtick}.`;
  }
  return `Matches ${backtick}.`;
}

function stripShortcutSuffix(text: string) {
  return text.replace(/\s*\((?:esc|escape|enter|return|space|[a-z0-9])\)\s*$/i, "");
}

function extractRequest(lines: CleanPromptLine[], summary?: string) {
  const candidates = lines
    .map((line) => line.text)
    .filter((line) => !NUMBERED_OPTION_RE.test(line))
    .filter((line) => !GROK_NUMBERED_OPTION_RE.test(line))
    .filter((line) => !looksLikePromptInstruction(line))
    .filter((line) => !looksLikeTerminalStatusLine(line))
    .filter((line) => !GROK_CHOICE_FOOTER_RE.test(line))
    .filter((line) => !/^action required$/i.test(line.trim()));

  for (let index = candidates.length - 1; index >= 0; index -= 1) {
    const line = candidates[index];
    const command = explicitCommandFromPromptLine(line);
    if (command) {
      return {
        label: "Command",
        text: truncateRunes(redactSensitiveText(command), 220),
      };
    }
  }

  const fallback = candidates[candidates.length - 1] || summary || "";
  const text = truncateRunes(redactSensitiveText(fallback.replace(/\s+/g, " ").trim()), 220);
  return {
    label: text ? "Request" : undefined,
    text,
  };
}

function explicitCommandFromPromptLine(line: string) {
  const trimmed = line.trim();
  const shellCommand = /^(?:[$#]\s+)(.+)$/.exec(trimmed)?.[1]?.trim();
  if (shellCommand) {
    return shellCommand;
  }

  const backtick = /`([^`]{1,500})`/.exec(trimmed)?.[1]?.trim();
  if (
    backtick &&
    /(?:run|execute|command|proceed|allow|approve|start)/i.test(trimmed)
  ) {
    return backtick;
  }

  return "";
}

export function redactSensitiveText(value: string) {
  return value
    .replace(/\b(Bearer\s+)[A-Za-z0-9._~+/=-]+/gi, "$1[redacted]")
    .replace(/\b(Basic\s+)[A-Za-z0-9+/=-]+/gi, "$1[redacted]")
    .replace(
      /\b([A-Z][A-Z0-9_]*(?:TOKEN|KEY|SECRET|PASSWORD|PASS|AUTH|CREDENTIAL)[A-Z0-9_]*)=("[^"]*"|'[^']*'|[^\s]+)/gi,
      "$1=[redacted]",
    )
    .replace(
      /(--?(?:api[-_]?key|token|password|passwd|secret|credential|auth(?:orization)?)(?:=|\s+))("[^"]*"|'[^']*'|[^\s]+)/gi,
      "$1[redacted]",
    )
    .replace(
      /([?&](?:token|api_key|apikey|key|password|secret|access_token|refresh_token|auth|signature)=)[^&\s]+/gi,
      "$1[redacted]",
    )
    .replace(/(https?:\/\/[^:\s/@]+:)[^@\s/]+@/gi, "$1[redacted]@")
    .replace(/\s+/g, " ")
    .trim();
}

function promptId(
  requestText: string | undefined,
  options: Array<Pick<TerminalActionPromptOption, "id" | "key" | "label">>,
) {
  return [
    requestText || "",
    ...options.map((option) => `${option.id}:${option.key}:${option.label}`),
  ].join("|");
}

function truncateRunes(value: string, maxLength: number) {
  const runes = Array.from(value);
  if (runes.length <= maxLength) {
    return value;
  }
  return `${runes.slice(0, Math.max(0, maxLength - 3)).join("")}...`;
}
