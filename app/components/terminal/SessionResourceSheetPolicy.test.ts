import { describe, expect, test } from "bun:test";
import type { SessionResourceSnapshot } from "../../services/sessionResourceSnapshot";
import {
  buildSessionResourceViewModel,
  resolveSessionResourceHostSections,
} from "./SessionResourceSheetModel";

function hostSections(snapshot: SessionResourceSnapshot) {
  const model = buildSessionResourceViewModel(snapshot);
  if (!model) throw new Error("expected Session resource presentation");
  return resolveSessionResourceHostSections(model.host);
}

describe("SessionResourceSheet host section policy", () => {
  test("places healthy availability only in a visible pool card", () => {
    const sections = hostSections({
      agent_id: "main:@7",
      session: {
        managed: true,
        backend: "cgroup_pool",
        memory_current_bytes: 2 * 1024 ** 3,
      },
      pool: {
        backend: "cgroup_pool",
        memory_current_bytes: 8 * 1024 ** 3,
        memory_max_bytes: 28 * 1024 ** 3,
      },
      host: { available_bytes: 23 * 1024 ** 3, pressure: "ok" },
    });

    expect(sections.poolSupport?.label).toBe("Host · 23.0 GiB available");
    expect(sections.footerSupport).toBeUndefined();
    expect(sections.warning).toBeUndefined();
  });

  test("places healthy availability in the footer without a pool card", () => {
    const sections = hostSections({
      agent_id: "main:@8",
      session: { managed: false },
      host: { available_bytes: 23 * 1024 ** 3, pressure: "ok" },
    });

    expect(sections.poolSupport).toBeUndefined();
    expect(sections.footerSupport?.label).toBe("Host · 23.0 GiB available");
    expect(sections.warning).toBeUndefined();
  });

  test("keeps unavailable headroom as subdued support instead of a warning", () => {
    const sections = hostSections({
      agent_id: "main:@9",
      session: { managed: true },
      host: { available_bytes: 23 * 1024 ** 3 },
    });

    expect(sections.poolSupport).toBeUndefined();
    expect(sections.footerSupport?.label).toBe(
      "Host · 23.0 GiB available · Headroom state unavailable",
    );
    expect(sections.warning).toBeUndefined();
  });

  test("reserves the warning section for confirmed pressure", () => {
    const sections = hostSections({
      agent_id: "main:@10",
      session: { managed: true, memory_current_bytes: 2048 },
      host: { available_bytes: 1025, pressure: "pressure" },
    });

    expect(sections.poolSupport).toBeUndefined();
    expect(sections.footerSupport).toBeUndefined();
    expect(sections.warning).toEqual({
      title: "Limited memory headroom",
      available: "1.00 KiB",
      availableExact: "1025 bytes",
      note: "Agents may wait for memory headroom",
      accessibilityLabel:
        "Limited memory headroom. Host available 1025 bytes. Agents may wait for memory headroom",
    });
  });
});
