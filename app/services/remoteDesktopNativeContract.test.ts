import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const android = readFileSync(new URL("../modules/zen-remote-desktop/android/src/main/java/expo/modules/zenremotedesktop/ZenRemoteDesktopModule.kt", import.meta.url), "utf8");
const ios = readFileSync(new URL("../modules/zen-remote-desktop/ios/ZenRemoteDesktopModule.swift", import.meta.url), "utf8");

// Source-contract checks only. Native toolchains and owned devices remain required.
describe("Remote desktop native lifecycle source contracts", () => {
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
    expect(metadata).toContain("queued.incrementAndGet() > 2");
    expect(metadata).toContain("finally { queued.decrementAndGet() }");
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
});
