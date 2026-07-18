import { describe, expect, test } from "bun:test";
import {
  bumpServerConnectionGeneration,
  isAgentSessionListFreshForConnection,
  stampAgentSessionListGeneration,
} from "./agentSessionListTransport";

describe("agentSessionListTransport", () => {
  test("stamps list generation only while connected; reconnect requires a new full list", () => {
    let connectionGenerationByServer = bumpServerConnectionGeneration(
      {},
      "server",
      "connecting",
      "connected",
    );
    expect(connectionGenerationByServer.server).toBe(1);
    expect(
      isAgentSessionListFreshForConnection({
        connectionState: "connected",
        connectionGeneration: 1,
        agentSessionListGeneration: 0,
      }),
    ).toBe(false);

    let listGens = stampAgentSessionListGeneration({
      connectionState: "connected",
      connectionGeneration: 1,
      agentSessionListGenerationByServer: {},
      serverId: "server",
    });
    expect(listGens.server).toBe(1);
    expect(
      isAgentSessionListFreshForConnection({
        connectionState: "connected",
        connectionGeneration: 1,
        agentSessionListGeneration: 1,
      }),
    ).toBe(true);

    // Disconnect does not clear retained gens; reconnect bumps connection gen.
    connectionGenerationByServer = bumpServerConnectionGeneration(
      connectionGenerationByServer,
      "server",
      "connecting",
      "connected",
    );
    expect(connectionGenerationByServer.server).toBe(2);
    expect(
      isAgentSessionListFreshForConnection({
        connectionState: "connected",
        connectionGeneration: 2,
        agentSessionListGeneration: listGens.server,
      }),
    ).toBe(false);

    // Incremental path: stamping while not matching still needs a full-list stamp.
    listGens = stampAgentSessionListGeneration({
      connectionState: "connecting",
      connectionGeneration: 2,
      agentSessionListGenerationByServer: listGens,
      serverId: "server",
    });
    expect(listGens.server).toBe(1);

    listGens = stampAgentSessionListGeneration({
      connectionState: "connected",
      connectionGeneration: 2,
      agentSessionListGenerationByServer: listGens,
      serverId: "server",
    });
    expect(listGens.server).toBe(2);
    expect(
      isAgentSessionListFreshForConnection({
        connectionState: "connected",
        connectionGeneration: 2,
        agentSessionListGeneration: 2,
      }),
    ).toBe(true);
  });
});
