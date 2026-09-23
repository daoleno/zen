import { expect, test } from "bun:test";
import { validateSvgPreview } from "./svgPreviewValidation";

test("bounded white vectors keep their original geometry", () => {
  const xml = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 4181 1472"><path fill="white" d="M0 0h4181v1472H0z"/></svg>`;
  const preview = validateSvgPreview(xml);
  expect(preview.ratio).toBeCloseTo(4181 / 1472);
  expect(preview.xml).toBe(xml);
});

test("malformed markup and external resources cannot reach native SVG rendering", () => {
  const wrap = (body: string) => `<svg viewBox="0 0 20 20" xmlns="http://www.w3.org/2000/svg">${body}</svg>`;
  expect(() => validateSvgPreview(wrap(`<path d="M0 0"`))).toThrow();
  expect(() => validateSvgPreview(`<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///secret">]>${wrap("<path d='M0 0'/>")}`)).toThrow();
  expect(() => validateSvgPreview(wrap(`<image href="https://example.test/tracker"/>`))).toThrow();
  expect(() => validateSvgPreview(wrap(`<foreignObject><script>bad()</script></foreignObject>`))).toThrow();
  expect(() => validateSvgPreview(wrap(`<path onPress="bad()" d="M0 0"/>`))).toThrow();
  expect(() => validateSvgPreview(wrap(`<path style="fill: url(https://example.test/track)" d="M0 0"/>`))).toThrow();
  expect(() => validateSvgPreview(`${wrap(`<path d="M0 0"/>`)}${wrap(`<path d="M0 0"/>`)}`)).toThrow();
  expect(() => validateSvgPreview(`${wrap(`<path d="M0 0"/>`)}<?xml-stylesheet href="https://example.test/track"?>`)).toThrow();
  expect(validateSvgPreview(wrap(`<defs><linearGradient id="fade"><stop offset="0" stop-color="white"/></linearGradient></defs><path fill="url(#fade)" d="M0 0"/>`)).ratio).toBe(1);
});
