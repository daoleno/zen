import { describe, expect, test } from "bun:test";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";

const moduleRoot = new URL("../modules/zen-remote-desktop/", import.meta.url);
const read = (relative: string) => readFileSync(new URL(relative, moduleRoot), "utf8");
const sha256 = (relative: string) =>
  createHash("sha256").update(readFileSync(new URL(relative, moduleRoot))).digest("hex");

const lock = JSON.parse(read("native.lock.json"));
const bridge = read("android/src/main/cpp/moonlight_bridge.c");
const core = read("android/src/main/java/expo/modules/zenremotedesktop/MoonlightCore.kt");
const cmake = read("android/CMakeLists.txt");
const gradle = read("android/build.gradle");
const thirdParty = read("THIRD_PARTY.md");

// These tests pin the *integration contract* only. A device/host run is still
// required to prove streaming; no test here fakes a frame or a connection.
describe("Moonlight client core integration contract", () => {
  test("pinned revisions and license digests are exact", () => {
    expect(lock.moonlight_common_c.commit).toBe("62e066388f1a1b133e0bee947b9a374311a3354b");
    expect(lock.moonlight_common_c.archive_sha256).toBe(
      "2486d3dfac30ae6bc68d362dac916fdb91294a12f82be649ebc00fba932cb8ed",
    );
    expect(lock.enet.commit).toBe("aca87840b57f045a1f7f9299e4b1b9b8e2a5e2f1");
    expect(lock.nanors.commit).toBe("b1e3c22ca0cdc0bb83e3cd6ed1a2fc77869ed99a");
    expect(lock.moonlight_common_c.license).toBe("GPL-3.0");
    expect(sha256("notices/GPL-3.0-moonlight-common-c.txt")).toBe(
      lock.moonlight_common_c.license_sha256,
    );
    expect(sha256("notices/ENET-MIT.txt")).toBe(lock.enet.license_sha256);
    expect(sha256("notices/NANORS-MIT.txt")).toBe(lock.nanors.license_sha256);
  });

  test("bridge translates the upstream API and implements no protocol itself", () => {
    for (const symbol of [
      "LiStartConnection",
      "LiStopConnection",
      "LiInterruptConnection",
      "LiSendKeyboardEvent2",
      "LiSendUtf8TextEvent",
      "LiSendMouseMoveEvent",
      "LiSendMousePositionEvent",
      "LiSendMouseButtonEvent",
      "LiSendScrollEvent",
      "LiSendHighResScrollEvent",
      "submitDecodeUnit",
      "LiInitializeStreamConfiguration",
      "LiInitializeConnectionCallbacks",
    ]) {
      expect(bridge).toContain(symbol);
    }
    // No shadow protocol or crypto may grow in the bridge; those stay upstream.
    expect(bridge).not.toContain("#include <openssl/");
    expect(bridge).not.toContain("rfb");
    for (const rtspMethod of ["ANNOUNCE", "DESCRIBE", "RTSP/1.0", "Content-Base"]) {
      expect(bridge).not.toContain(rtspMethod);
    }
    // The single call site is the JNI entry point, not a reimplementation.
    expect(bridge.match(/= LiStartConnection\(/g)?.length).toBe(1);
    expect(bridge).toContain('#include "Limelight.h"');
  });

  test("decode units are copied synchronously before the core frees them", () => {
    const submit = bridge.slice(
      bridge.indexOf("static int bridge_submit_decode_unit"),
      bridge.indexOf("/* ---- connection listener callbacks"),
    );
    expect(submit).toContain("NewByteArray");
    expect(submit).toContain("SetByteArrayRegion");
    expect(submit).toContain("CallIntMethod");
    expect(submit).toContain("DeleteLocalRef(env, frame)");
    // No stored pointer to core-owned buffers.
    expect(submit).not.toContain("malloc");
  });

  test("non-thread-safe lifecycle is serialized and interrupt-first", () => {
    // The bridge owns one live session: start may return while streams keep
    // running, and stop runs the single real LiStopConnection.
    expect(bridge).toContain("pthread_mutex_t g_lock");
    expect(bridge).toContain("pthread_cond_t g_state_cond");
    expect(bridge).toContain("bridge_stop_owned_session");
    expect(bridge).toContain("bridge_release_session_ref");
    expect(bridge).toContain("LiStopConnection()");
    expect(bridge).toContain("LiInterruptConnection()");
    expect(core).toContain("nativeStopConnection()");
    expect(core).toContain("nativeSessionState()");
    expect(core).toContain("Charsets.UTF_8");
    expect(core).toContain('System.loadLibrary("zen_moonlight")');
    // Native method names must match the Kotlin declarations exactly.
    for (const name of [
      "nativeStartConnection",
      "nativeStopConnection",
      "nativeInterruptConnection",
      "nativeIsActive",
      "nativeSessionState",
      "nativeLastError",
      "nativeSendKeyboardEvent",
      "nativeSendUtf8TextEvent",
      "nativeSendMouseMove",
      "nativeSendMousePosition",
      "nativeSendMouseButton",
      "nativeSendScroll",
      "nativeSendHighResScroll",
    ]) {
      expect(bridge).toContain(`Java_expo_modules_zenremotedesktop_MoonlightCore_${name}`);
      expect(core).toContain(name);
    }
  });

  test("build wiring compiles pinned sources and requires the OpenSSL prefix", () => {
    expect(cmake).toContain("third_party/moonlight-common-c");
    expect(cmake).toContain("add_subdirectory");
    expect(cmake).toContain("moonlight-common-c");
    expect(cmake).toContain("enet");
    expect(cmake).toContain("OpenSSL::Crypto");
    expect(cmake).toContain("third_party/openssl/${ANDROID_ABI}");
    expect(gradle).toContain("externalNativeBuild");
    expect(gradle).toContain("path 'CMakeLists.txt'");
    expect(gradle).toContain("abiFilters(*resolvedAbis)");
  });

  test("GPL corresponding-source obligations are documented", () => {
    expect(thirdParty).toContain("GPL-3.0");
    expect(thirdParty).toContain("62e066388f1a1b133e0bee947b9a374311a3354b");
    expect(thirdParty).toContain("scripts/fetch-moonlight-common-c.sh");
    expect(thirdParty).toContain("scripts/build-openssl-android.sh");
    expect(thirdParty).toContain("No upstream source file is patched");
  });
});
