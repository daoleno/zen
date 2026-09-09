import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const android = readFileSync(new URL("../modules/zen-remote-desktop/android/src/main/java/expo/modules/zenremotedesktop/ZenRemoteDesktopModule.kt", import.meta.url), "utf8");
const ios = readFileSync(new URL("../modules/zen-remote-desktop/ios/ZenRemoteDesktopModule.swift", import.meta.url), "utf8");
const route = readFileSync(new URL("../app/remote-desktop.tsx", import.meta.url), "utf8");
const prepare = readFileSync(new URL("./remoteDesktop.ts", import.meta.url), "utf8");
const androidFailure = readFileSync(new URL("../modules/zen-remote-desktop/android/src/main/java/expo/modules/zenremotedesktop/DesktopFailure.kt", import.meta.url), "utf8");

// Source-contract checks only. Native toolchains and owned devices remain required.
describe("Remote desktop native lifecycle source contracts", () => {
  test("imperative keyboard keys do not race native text-history updates", () => {
    expect(route).toContain('"Enter", () => input(desktopKey(0xff0d))');
    expect(route).toContain('"Backspace", () => input(desktopKey(0xff08))');
    expect(route).toContain('onSubmitEditing={() => input(desktopKey(0xff0d))}');
    expect(route).toContain('desktopTextEdits(textRef.current, value)');
    expect(route).toContain('KeyboardAvoidingView');
  });
  test("Android validates metadata before asynchronous decoder work", () => {
    const start = android.indexOf("override fun onMessage(ws: WebSocket, text: String)");
    const end = android.indexOf("override fun onMessage(ws: WebSocket, bytes: ByteString)");
    const metadata = android.slice(start, end);
    const dispatch = metadata.indexOf("decoder.post statusWork@");
    expect(dispatch).toBeGreaterThan(0);
    expect(metadata.indexOf('status.getString("state")')).toBeLessThan(dispatch);
    expect(metadata.indexOf('status.getInt("width")')).toBeLessThan(dispatch);
    expect(metadata.indexOf("require(nextWidth in 2..4096 && nextHeight in 2..4096)")).toBeLessThan(dispatch);
    expect(metadata.slice(dispatch)).not.toContain("status.getInt");
    expect(metadata.slice(dispatch)).not.toContain("status.getString");
    expect(metadata).toContain("admission.acquire({ epoch == generation.get() })");
    expect(metadata).toContain("finally { admission.release() }");
  });

  test("Android stop forgets reconnect credentials and covers retained frames", () => {
    const stop = android.slice(android.indexOf("private fun stop()"), android.indexOf("fun destroy()"));
    expect(stop).toContain('pendingConnection = ""');
    expect(stop).toContain("generation.incrementAndGet()");
    expect(stop).toContain("socket?.cancel()");
    expect(stop).toContain("cover.visibility = VISIBLE");
  });

  test("terminal native status ends ownership without waiting for socket closure", () => {
    expect(android).toContain('if (terminalState) {\n                    stop(); state(value, status.optString("reason")); return@publishStatus');
    expect(ios).toContain('if self.terminalState {\n              self.stop()\n              self.state(state, status["reason"] as? String ?? "")\n              return');
  });

  test("only native presentation can report connected", () => {
    const allowed = '["sources", "requesting", "streaming", "denied", "unsupported", "disconnected"]';
    expect(ios).toContain(`${allowed}.contains(state)`);
    expect(android).toContain('require(value in listOf("sources", "requesting", "streaming", "denied", "unsupported", "disconnected"))');
    expect(android).toContain("setOnFrameRenderedListener");
    expect(ios).toContain("layer.isReadyForDisplay");
  });

  test("Android counts UTF-8 bytes and includes new input in the queue limit", () => {
    expect(android).toContain("text.toByteArray(Charsets.UTF_8).size > 8192");
    expect(android).toContain("val bytes = value.toByteArray(Charsets.UTF_8).size");
    expect(android).toContain("value.isEmpty() || bytes > 8192");
    expect(android).toContain("active.queueSize() + bytes > 32 * 1024");
    expect(ios).toContain("pendingSends < 4");
  });

  test("iOS rejects incomplete dimensions and invalidates callbacks on stop", () => {
    expect(ios).toContain('(status["width"] == nil) == (status["height"] == nil)');
    expect(ios).toContain('guard let width = status["width"] as? Int, let height = status["height"] as? Int else');
    expect(ios).toContain("generation == self.epoch");
    expect(ios).toContain("epoch += 1");
    expect(ios).toContain("video.flushAndRemoveImage()");
  });

  test("native failures map distinct admission codes instead of generic authorization", () => {
    expect(android).not.toContain("Desktop authorization is required");
    expect(ios).not.toContain("Desktop authorization is required");
    expect(android).toContain("DesktopFailure.map");
    expect(ios).toContain("DesktopFailure.map");
    expect(androidFailure).toContain("desktop_tls_required");
    expect(androidFailure).toContain("desktop_scope_required");
    expect(androidFailure).toContain("host_setup_required");
    expect(android).toContain('request.header("X-Zen-Desktop-Mode", mode)');
    expect(ios).toContain('request.setValue(mode, forHTTPHeaderField: "X-Zen-Desktop-Mode")');
    expect(route).toContain("Attended unencrypted LAN (not lock/login)");
    expect(route).not.toContain("Allow unencrypted LAN desktop");
    expect(route).toContain("Grant unattended desktop");
    expect(prepare).toContain('mode: "unattended"');
    expect(prepare).toContain("desktopPinnedIdentityPlan");
    expect(prepare).toContain('throw new Error("Unattended desktop cannot use unencrypted LAN transport.")');
    const androidLink = readFileSync(new URL("../modules/zen-link-transport/android/src/main/java/expo/modules/zenlinktransport/ZenLinkTransportModule.kt", import.meta.url), "utf8");
    const iosLink = readFileSync(new URL("../modules/zen-link-transport/ios/ZenLinkTransportModule.swift", import.meta.url), "utf8");
    expect(androidLink).toContain('"zen-desktop.invalid"');
    expect(iosLink).toContain('"zen-desktop.invalid"');
    expect(androidLink).toContain("pinnedServerName");
    expect(iosLink).toContain("pinnedServerName");
  });
});
