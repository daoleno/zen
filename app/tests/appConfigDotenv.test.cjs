const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { createRequire } = require("node:module");
const configPath = path.resolve(__dirname, "../app.config.js");
const source = fs.readFileSync(configPath, "utf8");
const localRequire = createRequire(configPath);

function load(env, forbidRead) {
  let reads = 0;
  const module = { exports: {} };
  vm.runInNewContext(source, {
    __dirname: path.dirname(configPath), module, process: { env },
    require(name) {
      if (name !== "fs") return localRequire(name);
      return {
        existsSync: () => true,
        readFileSync: () => {
          reads++;
          if (forbidRead) throw new Error("Personal dotenv must not be read");
          return "ZEN_EXPO_PROJECT_ID=fixture-project\nPRIVATE_FIXTURE_VALUE=local-only\n";
        },
      };
    },
  }, { filename: configPath });
  return { config: module.exports(), reads, env };
}

test("EXPO_NO_DOTENV prevents custom config from reading personal dotenv", () => {
  const result = load({ EXPO_NO_DOTENV: "1" }, true);
  assert.equal(result.reads, 0);
  assert.equal(result.env.PRIVATE_FIXTURE_VALUE, undefined);
  assert.equal(result.config.extra.eas, undefined);
});

test("default config retains local dotenv behavior without overwriting explicit values", () => {
  const result = load({ ZEN_EXPO_PROJECT_ID: "explicit-project" }, false);
  assert.equal(result.reads, 1);
  assert.equal(result.env.PRIVATE_FIXTURE_VALUE, "local-only");
  assert.equal(result.config.extra.eas.projectId, "explicit-project");
});
