import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

describe("SessionModelSheet direct state", () => {
  const sheet = source("SessionModelSheet.tsx");

  test("renders a truthful read-only body for official-direct Sessions", () => {
    expect(sheet).toContain("nonRouted?: boolean;");
    expect(sheet).toContain("directClient?: SessionModelDirectClient | null;");
    expect(sheet).toContain("This Session is managed directly by the official");
    expect(sheet).toContain(
      "Zen cannot change its model or Base URL inside this",
    );
    expect(sheet).toContain("Configured providers");
    expect(sheet).toContain("Configure providers →");
    expect(sheet).toContain(
      "Model switching is available on Zen-launched Provider Sessions.",
    );
  });

  test("direct body never overlaps loading or error states", () => {
    expect(sheet).toContain(
      "{nonRouted && directClientName && !loading && !errorMessage ? (",
    );
  });

  test("direct rows are disabled and cannot fabricate activation", () => {
    expect(sheet).toContain("const canActivate =");
    expect(sheet).toContain("!nonRouted;");
    expect(sheet).toContain("disableAll,");
    expect(sheet).toContain("const disabled =\n                  disableAll ||");
  });

  test("routed selection rows still activate", () => {
    expect(sheet).toContain("{selection.connection_name}");
    expect(sheet).toContain("selection.model_id} · {clientLabel(selection.client)}");
    expect(sheet).toContain("onActivate({");
  });
});

describe("useSessionProviderSheet direct wiring", () => {
  const hook = source("../terminal/screen/useSessionProviderSheet.ts");

  test("accepts the client hint and returns the direct flag", () => {
    expect(hook).toContain("client?: DirectSessionClient | null;");
    expect(hook).toContain("const direct = !managed && isDirectSessionClient(client);");
    expect(hook).toContain("direct,");
    expect(hook).toContain("client,");
  });

  test("direct Sessions load only the Provider catalog with full guards", () => {
    expect(hook).toContain("setSheetMode(\"direct\")");
    expect(hook).toContain("admitCatalogLoad()");
    expect(hook).toContain("acceptCatalog(token, nextCatalog.revision)");
    expect(hook).toContain("await wsClient.listProviders(serverId)");
    expect(hook).toContain("direct,");
  });

  test("Composer control routes managed vs direct presentation", () => {
    expect(hook).toContain(
      "const composerControl: ComposerModelControlPresentation | null = managed",
    );
    expect(hook).toContain("resolveComposerModelControl({");
    expect(hook).toContain(
      ": resolveDirectComposerModelControl({ client, capabilities });",
    );
  });

  test("direct connections are filtered by client, routed by Session selection", () => {
    expect(hook).toContain("const filteredConnections: ProviderConnection[] = direct");
    expect(hook).toContain("connectionsForClient(catalog, client ?? \"\")");
    expect(hook).toContain(": connectionsForSession(catalog, selection);");
  });

  test("eager load never fabricates a Session selection for direct Sessions", () => {
    expect(hook).toContain("if (!managed || !serverId || !agentId || !connectionConnected) return;");
  });
});
