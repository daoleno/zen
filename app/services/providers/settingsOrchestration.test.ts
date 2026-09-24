import { describe, expect, test } from "bun:test";
import { launchSelectionFromSnapshot } from "./presentation";
import type { ProvidersSnapshot } from "./types";

describe("Agent connection selection", () => {
  const snapshot: ProvidersSnapshot = {
    revision: 1,
    defaults: {
      codex: { connection_id: "codex-connection" },
      claude: { connection_id: "claude-connection" },
    },
    presets: [],
    connections: [
      { id: "codex-connection", name: "Codex", clients: ["codex"], credential_ready: true, advanced: false },
      { id: "claude-connection", name: "Claude", clients: ["claude"], credential_ready: true, advanced: false },
    ],
    models: {},
  };

  test("neither Agent needs a Provider model catalog to select a connection", () => {
    expect(launchSelectionFromSnapshot(snapshot, "codex")).toEqual({
      connectionId: "codex-connection", modelId: "",
    });
    expect(launchSelectionFromSnapshot(snapshot, "claude")).toEqual({
      connectionId: "claude-connection", modelId: "",
    });
  });
});
