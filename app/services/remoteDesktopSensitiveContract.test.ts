import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const android = readFileSync(new URL("../modules/zen-remote-desktop/android/src/main/java/expo/modules/zenremotedesktop/ZenRemoteDesktopModule.kt", import.meta.url), "utf8");
const ios = readFileSync(new URL("../modules/zen-remote-desktop/ios/ZenRemoteDesktopModule.swift", import.meta.url), "utf8");
const route = readFileSync(new URL("../app/remote-desktop.tsx", import.meta.url), "utf8");

test("sensitive editors are native, scoped, ephemeral and do not export text to JS", () => {
  expect(android).toContain("InputType.TYPE_TEXT_VARIATION_PASSWORD");
  expect(android).toContain("IME_FLAG_NO_PERSONALIZED_LEARNING");
  expect(android).toContain("FLAG_SECURE");
  expect(android).toContain("PasswordTransformationMethod.getInstance()");
  expect(android.indexOf("setSingleLine(true)")).toBeLessThan(android.indexOf("inputType = InputType.TYPE_CLASS_TEXT"));
  expect(android).toContain("sensitiveEditor?.text?.clear()");
  expect(ios).toContain("field.isSecureTextEntry = true");
  expect(ios).toContain("sensitiveDialog?.textFields?.first?.text = nil");
  expect(android).toContain("owner != inputGeneration");
  expect(ios).toContain("self.epoch == generation");
  expect(route).toContain("showSensitiveInput?.(commands.currentGeneration)");
  expect(route).not.toContain('type: "sensitive"');
});

test("sensitive transport needs live native pin ownership or completed TLS", () => {
  expect(android).toContain("PinnedEndpointRegistry.contains");
  expect(android).toContain("response.handshake != null");
  expect(android).toContain('"tls" -> tlsConnected');
  expect(ios).toContain("PinnedEndpointRegistry.contains");
  expect(ios).toContain('url.scheme == "wss" && lastVideo != nil');
});

test("lock and greeter surfaces cannot open the JS history keyboard", () => {
  expect(route).toContain('status.surface === "greeter" || status.surface === "locked"');
  expect(route).toContain('nativeEvent.surface === "greeter" || nativeEvent.surface === "locked"');
  expect(android).toContain('.put("submit", true)');
  expect(ios).toContain('"submit": true');
});
