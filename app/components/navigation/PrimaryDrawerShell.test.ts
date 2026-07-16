import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = readFileSync(
  join(import.meta.dir, "PrimaryDrawerShell.tsx"),
  "utf8",
);

describe("primary drawer gesture activation", () => {
  test("keeps the closed drawer recognizer on a fixed leading edge", () => {
    expect(source).toContain("const PRIMARY_DRAWER_SWIPE_EDGE_WIDTH = 40;");
    expect(source).toContain(
      "swipeEdgeWidth={PRIMARY_DRAWER_SWIPE_EDGE_WIDTH}",
    );
    expect(source).not.toContain("swipeEdgeWidth={windowWidth}");
  });
});
