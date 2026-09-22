import { expect, test } from "bun:test";
import { skillImagePath } from "./skillImagePath";

test("Markdown image resolution stays in the exact advertised Skill copy", () => {
  const files = [{ path: "images/photo one.png" }, { path: "docs/screen.png" }];
  expect(skillImagePath("../images/photo%20one.png", "docs/guide.md", files)).toBe("images/photo one.png");
  expect(skillImagePath("screen.png", "docs/guide.md", files)).toBe("docs/screen.png");
  expect(skillImagePath("skill-file:images/photo one.png", "docs/guide.md", files)).toBe("images/photo one.png");
  for (const path of ["../../outside.png", "/private/photo.png", "missing.png", "file:///private/photo.png"]) {
    expect(() => skillImagePath(path, "docs/guide.md", files)).toThrow();
  }
});
