import { expect, test } from "bun:test";
import { imageReference, imageSourceKey, resolveImageSource } from "./imageSource";

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

test("bounded raster data is inspectable without external authorization", async () => {
  const controller = new AbortController();
  expect((await resolveImageSource(imageReference("data:image/png;base64,YQ=="), null, controller.signal)).headers).toEqual({});
  await expect(resolveImageSource(imageReference("data:image/svg+xml;base64,YQ=="), null, controller.signal)).rejects.toThrow("unsupported");
});
