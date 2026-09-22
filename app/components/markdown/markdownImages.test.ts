import { expect, test } from "bun:test";
import { splitMarkdownImages } from "./markdownImages";

test("extracts ordered images and captions, including reference images, without scanning code or URLs", () => {
  const markdown = 'before ![first](a.png "Caption") after\n\n`![code](bad.png)`\n\n![second][ref]\n\n[ref]: b.png\n\nhttps://example.test/plain.png';
  const segments = splitMarkdownImages(markdown);
  expect(segments.filter((part) => part.type === "image")).toEqual([
    { type: "image", path: "a.png", alt: "first", title: "Caption" },
    { type: "image", path: "b.png", alt: "second", title: undefined },
  ]);
  expect(segments[0]).toEqual({ type: "markdown", text: "before " });
  expect(segments.some((part) => part.type === "markdown" && part.text.includes("![code](bad.png)"))).toBe(true);
});
test("incomplete syntax and fenced image source remain text", () => {
  for (const value of ['![unfinished](foo', '```md\n![source](foo.png)\n```', '\\![escaped](foo.png)']) {
    expect(splitMarkdownImages(value)).toEqual([{ type: "markdown", text: value }]);
  }
});
