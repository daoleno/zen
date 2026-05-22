import React from "react";
import { Ionicons } from "@expo/vector-icons";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

export function slashCommandTitle(name: string) {
  return name
    .split("-")
    .filter(Boolean)
    .map((part) => {
      const lower = part.toLowerCase();
      if (lower === "ide" || lower === "mcp") {
        return lower.toUpperCase();
      }
      return `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`;
    })
    .join(" ");
}

export function slashCommandIcon(name: string): IoniconName {
  switch (name) {
    case "model":
      return "hardware-chip-outline";
    case "fast":
      return "flash-outline";
    case "ide":
      return "code-slash-outline";
    case "permissions":
    case "approve":
    case "test-approval":
      return "shield-checkmark-outline";
    case "keymap":
      return "keypad-outline";
    case "setup-default-sandbox":
    case "sandbox-add-read-dir":
      return "lock-open-outline";
    case "vim":
      return "create-outline";
    case "experimental":
      return "flask-outline";
    case "memories":
      return "library-outline";
    case "skills":
      return "sparkles-outline";
    case "hooks":
      return "link-outline";
    case "review":
      return "search-outline";
    case "rename":
    case "title":
      return "text-outline";
    case "new":
      return "add-circle-outline";
    case "resume":
      return "play-forward-outline";
    case "fork":
    case "side":
      return "git-branch-outline";
    case "init":
      return "document-text-outline";
    case "compact":
      return "contract-outline";
    case "plan":
      return "list-outline";
    case "goal":
      return "flag-outline";
    case "copy":
      return "copy-outline";
    case "raw":
      return "reorder-four-outline";
    case "diff":
      return "git-compare-outline";
    case "mention":
      return "at-outline";
    case "status":
      return "pulse-outline";
    case "debug-config":
    case "debug-m-drop":
    case "debug-m-update":
      return "bug-outline";
    case "statusline":
      return "reader-outline";
    case "theme":
      return "color-palette-outline";
    case "pets":
      return "happy-outline";
    case "mcp":
      return "server-outline";
    case "apps":
    case "plugins":
      return "extension-puzzle-outline";
    case "logout":
    case "quit":
    case "exit":
      return "exit-outline";
    case "feedback":
      return "chatbox-ellipses-outline";
    case "rollout":
      return "map-outline";
    case "ps":
      return "layers-outline";
    case "stop":
      return "stop-circle-outline";
    case "clear":
      return "trash-outline";
    case "personality":
      return "person-circle-outline";
    case "realtime":
    case "settings":
      return "mic-outline";
    case "agent":
    case "subagents":
      return "people-outline";
    case "btw":
      return "chatbubble-ellipses-outline";
    default:
      return "terminal-outline";
  }
}
