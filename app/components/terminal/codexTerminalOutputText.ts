const CODEX_CONTEXTUAL_FRAGMENT_MARKERS = [
  ["# AGENTS.md instructions for ", "</INSTRUCTIONS>"],
  ["<environment_context>", "</environment_context>"],
  ["<apps_instructions>", "</apps_instructions>"],
  ["<skills_instructions>", "</skills_instructions>"],
  ["<plugins_instructions>", "</plugins_instructions>"],
  ["<collaboration_mode>", "</collaboration_mode>"],
  ["<realtime_conversation>", "</realtime_conversation>"],
  ["<permissions instructions>", "</permissions instructions>"],
  ["<skill>", "</skill>"],
  ["<user_shell_command>", "</user_shell_command>"],
  ["<turn_aborted>", "</turn_aborted>"],
  ["<subagent_notification>", "</subagent_notification>"],
  ["<goal_context>", "</goal_context>"],
  ["<model_switch>", "</model_switch>"],
  ["<personality_spec>", "</personality_spec>"],
] as const;

export function compactCodexTerminalOutputLines(
  lines: string[],
  commandText: string = "",
) {
  return stripCodexContextualFragments(
    trimTerminalPromptTail(stripCommandEchoLines(lines, commandText))
      .map((line) => line.replace(/\s+$/g, ""))
      .filter((line, index, all) => {
        if (line.trim()) {
          return true;
        }
        return index > 0 && index < all.length - 1;
      })
      .join("\n")
      .trim(),
  ).slice(0, 5000);
}

export function cleanCodexTerminalOutputText(
  text: string | undefined,
  commandText: string = "",
) {
  if (!text?.trim()) {
    return "";
  }
  return compactCodexTerminalOutputLines(
    text.replace(/\r/g, "").split("\n"),
    commandText,
  );
}

function trimTerminalPromptTail(lines: string[]) {
  const normalized = lines.map((line) => line.trimEnd());
  for (let index = 0; index < normalized.length; index += 1) {
    const line = normalized[index]?.trim() || "";
    if (isCodexPromptLine(line) && index > 0) {
      return normalized.slice(0, index);
    }
  }
  return normalized;
}

function isCodexPromptLine(line: string) {
  return line.startsWith("› ") || line.startsWith("> ");
}

function stripCommandEchoLines(lines: string[], commandText: string) {
  const trimmedCommand = commandText.trim();
  if (!trimmedCommand) {
    return lines;
  }
  let started = false;
  return lines.filter((line) => {
    const trimmedLine = line.trim();
    if (!started && !trimmedLine) {
      return true;
    }
    started = true;
    return !(
      trimmedLine === trimmedCommand ||
      trimmedLine === `› ${trimmedCommand}` ||
      trimmedLine === `> ${trimmedCommand}`
    );
  });
}

function stripCodexContextualFragments(value: string) {
  let stripped = value.trim();
  if (!stripped) {
    return "";
  }
  let changed = true;
  while (changed) {
    changed = false;
    let best: { start: number; end: number } | null = null;
    for (const [open, close] of CODEX_CONTEXTUAL_FRAGMENT_MARKERS) {
      const range = markedTextRange(open, close, stripped);
      if (!range) {
        continue;
      }
      if (!best || range.start < best.start) {
        best = range;
      }
    }
    if (best) {
      stripped = `${stripped.slice(0, best.start)}\n${stripped.slice(best.end)}`.trim();
      changed = true;
    }
  }
  return stripped;
}

function markedTextRange(openMarker: string, closeMarker: string, value: string) {
  let searchFrom = 0;
  while (searchFrom < value.length) {
    const relativeStart = value.indexOf(openMarker, searchFrom);
    if (relativeStart < 0) {
      return null;
    }
    if (!isLineStartMarker(value, relativeStart)) {
      searchFrom = relativeStart + openMarker.length;
      continue;
    }
    const closeSearchFrom = relativeStart + openMarker.length;
    const relativeEnd = value.indexOf(closeMarker, closeSearchFrom);
    if (relativeEnd < 0) {
      return null;
    }
    return {
      start: relativeStart,
      end: relativeEnd + closeMarker.length,
    };
  }
  return null;
}

function isLineStartMarker(value: string, index: number) {
  for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
    const char = value[cursor];
    if (char === " " || char === "\t" || char === "\r") {
      continue;
    }
    return char === "\n";
  }
  return true;
}
