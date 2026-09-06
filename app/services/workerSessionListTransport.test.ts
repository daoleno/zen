import { describe, expect, test } from "bun:test";
import {
  bumpServerConnectionGeneration,
  isWorkerSessionListFreshForConnection,
  stampWorkerSessionListGeneration,
} from "./workerSessionListTransport";

describe("workerSessionListTransport", () => {
  test("stamps list generation only while connected; reconnect requires a new full list", () => {
    let connectionGenerationByServer = bumpServerConnectionGeneration(
      {},
      "server",
      "connecting",
      "connected",
    );
    expect(connectionGenerationByServer.server).toBe(1);
    expect(
      isWorkerSessionListFreshForConnection({
        connectionState: "connected",
        connectionGeneration: 1,
        workerSessionListGeneration: 0,
      }),
    ).toBe(false);

    let listGens = stampWorkerSessionListGeneration({
      connectionState: "connected",
      connectionGeneration: 1,
      workerSessionListGenerationByServer: {},
      serverId: "server",
    });
    expect(listGens.server).toBe(1);
    expect(
      isWorkerSessionListFreshForConnection({
        connectionState: "connected",
        connectionGeneration: 1,
        workerSessionListGeneration: 1,
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
      isWorkerSessionListFreshForConnection({
        connectionState: "connected",
        connectionGeneration: 2,
        workerSessionListGeneration: listGens.server,
      }),
    ).toBe(false);

    // Incremental path: stamping while not matching still needs a full-list stamp.
    listGens = stampWorkerSessionListGeneration({
      connectionState: "connecting",
      connectionGeneration: 2,
      workerSessionListGenerationByServer: listGens,
      serverId: "server",
    });
    expect(listGens.server).toBe(1);

    listGens = stampWorkerSessionListGeneration({
      connectionState: "connected",
      connectionGeneration: 2,
      workerSessionListGenerationByServer: listGens,
      serverId: "server",
    });
    expect(listGens.server).toBe(2);
    expect(
      isWorkerSessionListFreshForConnection({
        connectionState: "connected",
        connectionGeneration: 2,
        workerSessionListGeneration: 2,
      }),
    ).toBe(true);
  });
});
