import { expect, test } from "bun:test";
import { imageReference, imageSourceKey, isImageAttachment, isSvgImage, resolveImageSource } from "./imageSource";

test("phone grants remain intact and provider file URIs require their owner", async () => {
  const signal = new AbortController().signal;
  expect(await resolveImageSource({ kind: "phone", uri: "content://provider/opaque%3Aid", name: "Photo" }, null, signal)).toEqual({ uri: "content://provider/opaque%3Aid", headers: {} });
  expect(imageReference("file:///private/image.png").kind).toBe("owned");
  await expect(resolveImageSource(imageReference("/private/image.png"), null, signal)).rejects.toThrow("Session");
});
test("external URLs carry no credentials and stale resolution is discarded", async () => {
  const controller = new AbortController();
  expect((await resolveImageSource(imageReference("https://example.test/image.png"), null, controller.signal)).headers).toEqual({});
  await expect(resolveImageSource(imageReference("https://user:pass@example.test/a.png"), null, controller.signal)).rejects.toThrow("Unsupported");
  await expect(resolveImageSource(imageReference("a.png"), { key: "server-a", resolve: async () => { controller.abort(); return { uri: "capability", headers: {} }; } }, controller.signal)).rejects.toThrow();
  expect(imageSourceKey(imageReference("a.png"), "server-a")).not.toBe(imageSourceKey(imageReference("a.png"), "server-b"));
});

test("bounded image data and SVG metadata retain their owner", async () => {
  const controller = new AbortController();
  expect((await resolveImageSource(imageReference("data:image/png;base64,YQ=="), null, controller.signal)).headers).toEqual({});
  const inline = imageReference("data:image/svg+xml;base64,YQ==");
  expect(isSvgImage(inline)).toBe(true);
  expect((await resolveImageSource(inline, null, controller.signal)).headers).toEqual({});
  expect(isImageAttachment({ name: "logo.svg" })).toBe(true);
  expect(isSvgImage({ kind: "phone", uri: "content://picker/grant", name: "logo", mimeType: "image/svg+xml" })).toBe(true);
  expect(isSvgImage(imageReference("/workspace/logo.svg"))).toBe(true);
  expect(isSvgImage(imageReference("opaque-reference", "Logo"), { uri: "https://owned.example.test/stream", headers: {}, mimeType: "image/svg+xml" })).toBe(true);
  await expect(resolveImageSource(imageReference("/workspace/logo.svg"), null, controller.signal)).rejects.toThrow("Session");
  await expect(resolveImageSource(imageReference("data:image/svg+xml;base64,%00"), null, controller.signal)).rejects.toThrow("unsupported");
});
