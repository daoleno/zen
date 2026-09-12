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

  test("vendored upstream application layer keeps pairing crypto and pinning semantics", () => {
    expect(lock.application_layer.commit).toBe("98c12bebffac592eb57cf25e9a4638b40aa2c17d");
    expect(lock.application_layer.license).toBe("GPL-3.0");
    const pairing = read(
      "android/src/main/java/expo/modules/zenremotedesktop/moonlight/PairingManager.java",
    );
    // Key exchange stays upstream code, not a Zen re-implementation.
    expect(pairing).toContain("AESLightEngine");
    expect(pairing).toContain("saltPin");
    expect(pairing).toContain("SHA256withRSA");
    expect(pairing).toContain("PIN_WRONG");
    const http = read(
      "android/src/main/java/expo/modules/zenremotedesktop/moonlight/MoonlightNvHttp.java",
    );
    // TLS chain validation first, exact pinned certificate second.
    expect(http).toContain("defaultTrustManager.checkServerTrusted");
    expect(http).toContain("Certificate mismatch");
    // Upstream pairing endpoint and verbs; no --config style flags.
    expect(http).toContain('"pair"');
    expect(http).toContain("request.verb()");
    expect(http).not.toContain('"--config"');
    const session = read(
      "android/src/main/java/expo/modules/zenremotedesktop/moonlight/ZenMoonlightSession.kt",
    );
    // The launched RI key material is what the core receives.
    expect(session).toContain("remoteInputAesKey = riKey");
    expect(session).toContain("remoteInputAesIv = riIv");
    expect(session).toContain("pairing_required");
    expect(session).toContain("launch_rejected");
    expect(session).toContain("invalid_video_formats");
    expect(session).toContain("startAccepted = startResult == 0");
  });

  test("paired renderer wiring and Review1121 corrections stay in production", () => {
    const session = read(
      "android/src/main/java/expo/modules/zenremotedesktop/moonlight/ZenMoonlightSession.kt",
    );
    // Defect 1: saved pin installed before the first authenticated request.
    expect(session.indexOf("host.setServerCert(saved)")).toBeGreaterThan(-1);
    expect(session.indexOf("host.setServerCert(saved)")).toBeLessThan(session.indexOf("host.fetchServerInfo()"));
    expect(session).toContain("trust_store_corrupt");
    // Defect 2: epoch fence and explicit revoke report.
    expect(session).toContain("attemptAlive");
    expect(session).toContain("cancelInFlight");
    expect(session).toContain("RevokeReport");
    expect(session).toContain("inMemoryPin");
    // Defect 3: upstream launch/resume/foreign-busy policy.
    expect(session).toContain("VERB_RESUME");
    expect(session).toContain("host_busy_foreign_app");
    // Defect 4: renderable decoder formats, never a raw SCM intersection.
    expect(session).toContain("MoonlightVideoFormats.isRenderable");
    expect(session).not.toContain("serverCodecModeSupport().toInt() == 0");
    const formats = read(
      "android/src/main/java/expo/modules/zenremotedesktop/moonlight/MoonlightVideoFormats.kt",
    );
    expect(formats).toContain("const val AV1_MAIN8 = 0x1000");
    expect(formats).toContain("const val SCM_AV1_MAIN8 = 0x00010000");
    const core = read("android/src/main/java/expo/modules/zenremotedesktop/MoonlightCore.kt");
    expect(core).toContain("const val VIDEO_FORMAT_AV1_MAIN8 = 0x1000");

    // Actual module -> MediaCodec/Surface + input/disconnect wiring.
    const module = read("android/src/main/java/expo/modules/zenremotedesktop/ZenRemoteDesktopModule.kt");
    expect(module).toContain("View(MoonlightDesktopView::class)");
    for (const fn of ["disconnect", "revoke", "sendKey", "sendText", "sendPointerMove", "sendPointerButton", "sendScroll"]) {
      expect(module).toContain(`AsyncFunction("${fn}")`);
    }
    const view = read("android/src/main/java/expo/modules/zenremotedesktop/MoonlightDesktopView.kt");
    expect(view).toContain('MediaCodec.createDecoderByType("video/avc")');
    expect(view).toContain("setOnFrameRenderedListener");
    expect(view).toContain("MAX_QUEUED_FRAMES");
    expect(view).toContain('"start_accepted"');
    expect(view).toContain('publish(conn, "frame", "first")');
    expect(view).toContain("sendUtf8TextEvent");
    expect(view).toContain("activeSession.revoke(hostClient)");
    const index = read("src/index.ts");
    expect(index).toContain("NativeMoonlightDesktopView");
    expect(index).toContain("MoonlightDesktopApi");

    // Actual App entry: the route mounts the Moonlight view when the daemon
    // advertises a Sunshine-capable host and routes input/disconnect/revoke.
    const route = read("../../app/remote-desktop.tsx");
    expect(route).toContain("NativeMoonlightDesktopView");
    expect(route).toContain('transport === "moonlight"');
    expect(route).toContain("moonlightInput");
    expect(route).toContain("sendPointerPosition");
    expect(route).toContain("sendPointerButton");
    expect(route).toContain("sendText");
    expect(route).toContain("sendKey");
    expect(route).toContain("moonlight.current?.revoke");
    expect(route).toContain("moonlight.current?.disconnect");
    expect(route).toContain("moonlightPin");
    const service = read("../../services/remoteDesktop.ts");
    expect(service).toContain("capability.moonlight?.available");
    expect(service).toContain('transport: "moonlight"');
    const capability = read("../../services/desktopConnectionCheck.ts");
    expect(capability).toContain("MoonlightHostBootstrap");
    expect(capability).toContain("identityKey");
    // Ownership/recovery wiring in the native view.
    expect(view).toContain("publishTerminal");
    expect(view).toContain("alive(conn)");
    expect(view).toContain("scheduleDrain");
    expect(view).toContain("recoverDecoder");
    expect(view).toContain('return -1');
    expect(view).toContain("conn.waitingIdr");
    expect(view).toContain("hostDirectory");
  });
});
