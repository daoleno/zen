import { describe, expect, test } from "bun:test";
import {
  dshWebServiceURL,
  hasServiceTerminal,
  isDSHWebService,
  isPersistentService,
  serviceSourceLabel,
  serviceWorkerLabel,
  type DiscoveredSessionService,
} from "./sessionServicesPresentation";

function sessionRow(): DiscoveredSessionService {
  return {
    id: "main:@1:10:3000",
    worker_id: "main:@1",
    worker_name: "Main",
    pid: 10,
    port: 3000,
    protocol: "tcp",
    binds: ["0.0.0.0"],
    urls: [],
    local_only: false,
    source: "session",
    serverId: "server-1",
    serverName: "zen",
  };
}

function persistentRow(): DiscoveredSessionService {
  return {
    id: "persistent:dsh-web.service:1610722:3080",
    worker_id: "",
    worker_name: "DeepSeek Harness",
    pid: 1610722,
    port: 3080,
    protocol: "tcp",
    binds: ["127.0.0.1"],
    urls: [
      {
        label: "LAN",
        url: "http://192.168.1.42:3080",
        address: "192.168.1.42",
        kind: "lan",
      },
    ],
    local_only: false,
    source: "persistent",
    unit: "dsh-web.service",
    state: "active",
    serverId: "server-1",
    serverName: "zen",
  };
}

describe("session service source", () => {
  test("projects DSH Web only from an active registered service URL", () => {
    const service = persistentRow();
    expect(isDSHWebService(service)).toBe(true);
    expect(dshWebServiceURL([service])).toBe("http://192.168.1.42:3080");
    expect(dshWebServiceURL([{ ...service, state: "inactive" }])).toBeNull();
    expect(dshWebServiceURL([{ ...service, urls: [] }])).toBeNull();
    expect(dshWebServiceURL([{ ...service, unit: "other.service" }])).toBeNull();
  });
  test("session rows keep a live terminal target", () => {
    const service = sessionRow();
    expect(isPersistentService(service)).toBe(false);
    expect(hasServiceTerminal(service)).toBe(true);
    expect(serviceSourceLabel(service)).toBe("Session");
    expect(serviceWorkerLabel(service)).toBe("Main");
  });

  test("persistent rows never acquire a terminal action", () => {
    const service = persistentRow();
    expect(isPersistentService(service)).toBe(true);
    expect(hasServiceTerminal(service)).toBe(false);
    expect(serviceSourceLabel(service)).toBe("Persistent · dsh-web.service");
    // No invented worker id behind the label.
    expect(service.worker_id).toBe("");
    expect(serviceWorkerLabel(service)).toBe("DeepSeek Harness");
  });

  test("inactive and error states stay honest in the label", () => {
    expect(
      serviceSourceLabel({ ...persistentRow(), state: "inactive" }),
    ).toBe("Persistent · dsh-web.service · Inactive");
    expect(serviceSourceLabel({ ...persistentRow(), state: "error" })).toBe(
      "Persistent · dsh-web.service · Error",
    );
  });

  test("missing worker id never yields a terminal target", () => {
    expect(hasServiceTerminal({ ...sessionRow(), worker_id: "" })).toBe(false);
  });
});
