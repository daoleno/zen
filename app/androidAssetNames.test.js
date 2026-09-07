import { expect, test } from "bun:test";
import { readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { getAndroidResourceFolderName, getAndroidResourceIdentifier } from "@react-native/assets-registry/path-support";
import { parse } from "metro/private/node-haste/lib/AssetPaths";

function collisionsFor(files) {
  const resources = new Map();
  const collisions = [];
  for (const file of files) {
    const parsed = parse(file, new Set(["android", "ios"]));
    if (parsed.platform === "ios") continue;
    const asset = {
      httpServerLocation: `/assets/assets/${dirname(file)}`,
      name: parsed.name,
      type: parsed.type,
    };
    const resource = `${getAndroidResourceFolderName(asset, parsed.resolution)}/${getAndroidResourceIdentifier(asset)}`;
    if (resources.has(resource)) {
      collisions.push([resources.get(resource), file, resource]);
    }
    resources.set(resource, file);
  }
  return collisions;
}

test("repository image and font assets have distinct Android resource names", () => {
  const directory = join(dirname(fileURLToPath(import.meta.url)), "assets");
  const files = readdirSync(directory, { recursive: true }).filter((file) =>
    /\.(png|jpe?g|gif|heic|heif|ktx|webp|xml|svg|ttf|otf)$/i.test(file),
  );
  expect(files.length).toBeGreaterThan(0);
  const collisions = collisionsFor(files);
  expect(collisions).toEqual([]);
});

test("the gate catches extension, punctuation and case collisions using RN normalization", () => {
  for (const files of [
    ["fixture/large.png", "fixture/large.jpg"],
    ["fixture/big-image.png", "fixture/bigimage.png"],
    ["fixture/Photo.png", "fixture/photo.png"],
    ["some-dir/image.png", "somedir/image.png"],
  ]) {
    expect(collisionsFor(files)).toHaveLength(1);
  }
  expect(collisionsFor(["fixture/image.png", "fixture/image@2x.png"])).toEqual([]);
});
