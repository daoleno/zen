import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const android = readFileSync(new URL("../modules/zen-remote-desktop/android/src/main/java/expo/modules/zenremotedesktop/ZenRemoteDesktopModule.kt", import.meta.url), "utf8");
const androidKeyboard = readFileSync(new URL("../modules/zen-remote-desktop/android/src/main/java/expo/modules/zenremotedesktop/DesktopKeyboardView.kt", import.meta.url), "utf8");
const keyboardPolicy = readFileSync(new URL("../modules/zen-remote-desktop/android/src/main/java/expo/modules/zenremotedesktop/DesktopKeyboardPolicy.kt", import.meta.url), "utf8");
const ios = readFileSync(new URL("../modules/zen-remote-desktop/ios/ZenRemoteDesktopModule.swift", import.meta.url), "utf8");
const route = readFileSync(new URL("../app/remote-desktop.tsx", import.meta.url), "utf8");
const keyboardModule = readFileSync(new URL("../modules/zen-remote-desktop/src/index.ts", import.meta.url), "utf8");
const prepare = readFileSync(new URL("./remoteDesktop.ts", import.meta.url), "utf8");
const androidFailure = readFileSync(new URL("../modules/zen-remote-desktop/android/src/main/java/expo/modules/zenremotedesktop/DesktopFailure.kt", import.meta.url), "utf8");

// Source-contract checks only. Native toolchains and owned devices remain required.
describe("Remote desktop native lifecycle source contracts", () => {
  test("native keyboard event names match the JS props exactly", () => {
    // Both platforms register the same two names; JS must consume those names,
    // not an alias that React Native would silently drop.
    expect(android).toContain('Events("onDesktopText", "onDesktopKey")');
    expect(ios).toContain('Events("onDesktopText", "onDesktopKey")');
    const names = ["onDesktopText", "onDesktopKey"];
    expect(keyboardModule).toContain('DESKTOP_KEYBOARD_EVENTS = ["onDesktopText", "onDesktopKey"]');
    for (const name of names) {
      expect(keyboardModule).toContain(`${name}:`);
      expect(route).toContain(`${name}={`);
    }
    expect(keyboardModule).not.toContain("onText:");
    expect(keyboardModule).not.toContain("onKey:");
    expect(route).not.toContain("onText={");
    expect(route).not.toContain("onKey={");
  });
  test("phone IME composing text never reaches the remote host", () => {
    // Commit-aware native capture: only commitText / committed delegate text
    // and named editing keys cross the wire; pinyin and candidates stay local.
    const composing = androidKeyboard.slice(androidKeyboard.indexOf("override fun setComposingText"), androidKeyboard.indexOf("override fun finishComposingText"));
    expect(composing).toContain("super.setComposingText");
    expect(composing).not.toContain("dispatchText");
    expect(androidKeyboard).toContain("override fun commitText");
    expect(androidKeyboard).toContain("private fun dispatchText(value: String)");
    expect(keyboardPolicy).toContain('const val KEY_BACKSPACE = "Backspace"');
    expect(keyboardPolicy).toContain('const val KEY_DELETE = "Delete"');
    expect(keyboardPolicy).toContain('const val KEY_ENTER = "Enter"');
    expect(androidKeyboard).toContain("dispatchKey(mapped.key)");
    expect(androidKeyboard).toContain("DesktopKeyboardPolicy.KEY_ENTER");
    expect(ios).toContain("shouldChangeCharactersIn");
    expect(ios).toContain('onDesktopText(["value": string])');
    expect(ios).toContain('onDesktopKey(["key": "Backspace"])');
    expect(route).toContain('sendNamedKey("Enter")');
    expect(route).toContain('sendNamedKey("Backspace")');
    expect(route).toContain("sendCommittedText(nativeEvent.value)");
    expect(route).toContain("desktopCommittedText(value)");
    expect(androidKeyboard).toContain('dispatchKey(it)');
    expect(route).toContain("nativeEvent.inputError");
    expect(android).toContain('status.optString("inputError", "")');
    expect(ios).toContain('status["inputError"] as? String');
    // Pre-native shell fallback stays explicit and bounded; it never sends
    // composing text as committed text on purpose.
    expect(route).toContain('desktopTextEdits(textRef.current, value)');
    expect(route).toContain('NativeDesktopKeyboard');
    expect(route).toContain('MaterialIcons name="keyboard"');
    expect(route).not.toContain('"keypad-outline"');
    expect(route).toContain('Keyboard.addListener("keyboardDidHide"');
    expect(route).toContain('KeyboardAvoidingView');
  });
  test("Android hardware and IME key paths share one dispatch point", () => {
    // Hardware/injected keys never reach the InputConnection; the editor's own
    // dispatchKeyEvent delegates to the pure policy, and the InputConnection
    // must not dispatch a second time.
    expect(androidKeyboard).toContain("override fun dispatchKeyEvent");
    expect(androidKeyboard).toContain("DesktopKeyboardPolicy.hardwareAction");
    expect(keyboardPolicy).toContain("KEYCODE_FORWARD_DEL");
    expect(keyboardPolicy).toContain("KEYCODE_NUMPAD_ENTER");
    expect(keyboardPolicy).toContain("Character.toChars(unicodeChar)");
    expect(androidKeyboard).toContain("return super.sendKeyEvent(event)");
    // Committed/pasted text arrives as one Editable change; composing spans
    // stay local and one commit cannot dispatch twice.
    expect(androidKeyboard).toContain("addTextChangedListener");
    expect(androidKeyboard).toContain("BaseInputConnection.getComposingSpanStart");
    expect(androidKeyboard).toContain("quietEdit");
    // finishComposingText is span-only and can never rely on the watcher.
    expect(androidKeyboard).toContain("override fun finishComposingText");
    expect(keyboardPolicy).toContain("fun finishedComposingText(");
    // Local preedit deletes stay local; committed deletes keep the exact
    // requested count with no time-based throttle.
    expect(androidKeyboard).toContain("DesktopKeyboardPolicy.deleteKeys");
    expect(keyboardPolicy).toContain("fun deleteKeys(");
    // One dispatch point per request: no sticky boolean and no posted clear
    // that could erase remaining preedit or a newer edit.
    expect(keyboardPolicy).not.toContain("consumesDuplicateDelete");
    expect(androidKeyboard).not.toContain("suppressNextDeleteKeyEvent");
    expect(androidKeyboard).not.toContain("SystemClock");
    expect(keyboardPolicy).not.toContain("SystemClock");
    const deleteWire = androidKeyboard.slice(androidKeyboard.indexOf("override fun deleteSurroundingText"), androidKeyboard.indexOf("override fun sendKeyEvent"));
    expect(deleteWire).not.toContain("clearText");
    expect(deleteWire).not.toContain("post {");
    // Landscape soft keyboard must stay docked instead of covering the video.
    expect(androidKeyboard).toContain("IME_FLAG_NO_EXTRACT_UI");
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
    expect(ios).toContain("#available(iOS 17.4");
    expect(ios).toContain("video.isReadyForDisplay");
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
    expect(route).not.toContain("Attended unencrypted LAN (not lock/login)");
    expect(route).not.toContain("Allow unencrypted LAN desktop");
    expect(route).toContain("Enable remote desktop");
    expect(route).toContain('This server requested attended sharing. Update Zen on the computer.');
    expect(route).not.toContain('desktopStart(');
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
