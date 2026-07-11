// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { fetchWindowScenePage } from "./windowScenes";

describe("windowScenes", () => {
  test("ships a zero-config rotating MP4 catalog", async () => {
    const first = await fetchWindowScenePage(null);
    const second = await fetchWindowScenePage(first.cursor);
    expect(first.scenes).toHaveLength(12);
    expect(second.scenes).toHaveLength(12);
    expect(first.scenes[0].id).not.toBe(second.scenes[0].id);
    for (const scene of [...first.scenes, ...second.scenes]) {
      expect(scene.uri).toMatch(/^https:\/\/assets\.mixkit\.co\/videos\/\d+\/\d+-720\.mp4$/);
      expect(scene.license).toBe("Mixkit Free License");
    }
  });
});
