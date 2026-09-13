/** A pinned caller reproduction, not a model evaluator or current product gate. */
import { strict as assert } from "node:assert";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = fileURLToPath(new URL("../../..", import.meta.url));
const revision = "558db054675c2e4eb44d1e6b87f0f20bda7b91dd";
const files = [
  "app/package.json",
  "app/services/auth.ts",
  "app/services/remoteDesktop.ts",
  "app/services/pinnedTransport.ts",
  "app/services/desktopConnectionCheck.ts",
  "app/services/desktopTransportPolicy.ts",
  "app/services/deviceAuthContract.ts",
  "app/services/protocolCrypto.ts",
  "app/modules/zen-remote-desktop/src/index.ts",
  "app/modules/zen-link-transport/src/index.ts",
  "app/test-support/remoteDesktopServiceHarness.ts",
  "daemon/server/desktop_moonlight_enroll.go",
];

const mode = process.argv[2] ?? "--replay";
assert.ok(["--replay", "--prepare"].includes(mode), "Use --replay or --prepare");
assert.equal(process.argv.length, process.argv[2] ? 3 : 2);
const root = mkdtempSync(join(tmpdir(), "brain-enrollment-"));
let retained = false;
try {
  for (const path of files) {
    const bytes = execFileSync("git", ["show", `${revision}:${path}`], {
      cwd: repo, timeout: 5000, maxBuffer: 1024 * 1024,
    });
    const target = join(root, path);
    mkdirSync(dirname(target), { recursive: true });
    writeFileSync(target, bytes);
    assert.deepEqual(readFileSync(target), bytes);
  }
  // Installed dependencies are reused, never installed or edited by this replay.
  symlinkSync(resolve(repo, "node_modules"), join(root, "node_modules"), "dir");
  writeFileSync(join(root, ".brain-engineering-fixture.json"), JSON.stringify({ revision }));
  if (mode === "--prepare") {
    console.log(JSON.stringify({ revision, root, files, cleanup: "Remove only this generated root after collecting evidence." }));
    retained = true;
  } else {
    const old = execFileSync(process.execPath, [join(root, "app/test-support/remoteDesktopServiceHarness.ts")], {
      cwd: root, timeout: 15000, encoding: "utf8", env: { ...process.env, NODE_ENV: "test" },
    });
    const oldResult = JSON.parse(old.trim());
    assert.equal(oldResult.enrollment, "verified");
    const caller = Bun.spawnSync({
      cmd: [process.execPath, fileURLToPath(new URL("engineering-enrollment-caller.ts", import.meta.url)), root],
      cwd: root, timeout: 15000, stdout: "pipe", stderr: "pipe",
      env: { ...process.env, NODE_ENV: "test" },
    });
    // The historical implementation MUST fail the user contract. Do not turn
    // this negative into a claim that enrollment or engineering judgment passes.
    const observed = JSON.parse(caller.stdout.toString().trim());
    console.log(JSON.stringify({ revision, oldHarness: oldResult, callerExit: caller.exitCode, observed }));
    if (caller.stderr.length) process.stderr.write(caller.stderr);
    assert.equal(caller.exitCode, 1, "Expected a reproduced contract failure, not a launch/tool failure");
    assert.equal(observed.jsonPending.value, "pending");
    assert.equal(observed.textPending.error, "SyntaxError");
    assert.equal(observed.textForbidden.error, "SyntaxError");
    assert.equal(observed.unsignedForeign.value, "verified");
    assert.equal(observed.proofChecks, 0);

    // A reference repair applies ONLY to our disposable historical copy. It
    // validates the exercise's sensitivity; it is not a desktop product fix.
    const patch = fileURLToPath(new URL("engineering-enrollment-reference.patch", import.meta.url));
    execFileSync("git", ["apply", "--check", patch], { cwd: root, timeout: 5000 });
    execFileSync("git", ["apply", patch], { cwd: root, timeout: 5000 });
    const repaired = execFileSync(process.execPath, [
      fileURLToPath(new URL("engineering-enrollment-caller.ts", import.meta.url)), root,
    ], { cwd: root, timeout: 15000, encoding: "utf8", env: { ...process.env, NODE_ENV: "test" } });
    const after = JSON.parse(repaired.trim());
    console.log(JSON.stringify({ referenceOnly: true, callerExit: 0, observed: after }));
    assert.equal(after.jsonPending.value, "pending");
    assert.equal(after.textPending.value, "pending");
    assert.equal(after.textForbidden.status, 403);
    assert.equal(after.unsignedForeign.value, "verified", "Identity validation is an intentionally unresolved boundary");
    assert.equal(after.proofChecks, 0);
  }
} finally {
  if (!retained) rmSync(root, { recursive: true, force: true });
}
