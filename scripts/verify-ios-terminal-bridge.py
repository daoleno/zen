#!/usr/bin/env python3
"""Static contract checks for the iOS bridge on non-Apple hosts.

This intentionally does not claim Swift, ObjC++, CocoaPods, or Xcode verification.
"""

import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
MODULE = ROOT / "app/modules/zen-terminal-vt"
IOS = MODULE / "ios"
HEADERS = MODULE / "android/src/main/cpp/ghostty"


def fail(message: str) -> None:
    raise AssertionError(message)


header_text = "\n".join(path.read_text() for path in sorted(HEADERS.rglob("*.h")))
bridge = (IOS / "ZenTerminalVtBridge.mm").read_text()
objc = (IOS / "ZenTerminalVtBridge.h").read_text()
swift = (IOS / "ZenTerminalVtModule.swift").read_text()
typescript = (MODULE / "src/index.ts").read_text()
android = (MODULE / "android/src/main/java/expo/modules/zenterminalvt/ZenTerminalVtModule.kt").read_text()
podspec = (IOS / "ZenTerminalVt.podspec").read_text()
app_config = (ROOT / "app/app.config.js").read_text()
deployment_plugin = (ROOT / "app/plugins/withZenIOSDeploymentTarget.js").read_text()

# Every referenced pinned C function, Ghostty type, enum, and ABI macro must be
# declared by the checked-in vt.h include tree. Local TerminalState is excluded.
symbols = set(re.findall(r"\bghostty_[a-z0-9_]+\b", bridge))
symbols |= set(re.findall(r"\bGhostty[A-Za-z0-9_]+\b", bridge))
symbols |= set(re.findall(r"\bGHOSTTY_[A-Z0-9_]+\b", bridge))
for symbol in sorted(symbols):
    if symbol not in header_text:
        fail(f"iOS bridge references symbol absent from pinned vt.h: {symbol}")

# NS_SWIFT_NAME is the single canonical selector mapping. Check both the ObjC
# declaration and the Swift call label sequence for every bridge operation.
selectors = {
    "createTerminal(columns:rows:)": "createTerminal(columns: cols, rows: rows)",
    "destroyTerminal(handle:)": "destroyTerminal(handle: handle)",
    "writeData(_:handle:)": "writeData(data, handle: handle)",
    "scrollViewport(_:handle:)": "scrollViewport(delta, handle: handle)",
    "scrollViewportToBottom(handle:)": "scrollViewportToBottom(handle: handle)",
    "resize(handle:columns:rows:cellWidth:cellHeight:)": "resize(\n        handle: handle,",
    "setTheme(handle:foreground:background:cursor:palette:)": "setTheme(\n        handle: handle,",
    "encodeMouseEvent(handle:action:button:x:y:mods:anyButtonPressed:)": "encodeMouseEvent(\n        handle: handle,",
    "renderSnapshot(handle:)": "renderSnapshot(handle: handle)",
    "visibleText(handle:)": "visibleText(handle: handle)",
    "visibleHTML(handle:)": "visibleHTML(handle: handle)",
}
for selector, call in selectors.items():
    if f"NS_SWIFT_NAME({selector})" not in objc:
        fail(f"missing explicit ObjC-to-Swift selector: {selector}")
    if call not in swift:
        fail(f"Swift does not call declared selector: {selector}")

# JS-required methods must exist on Android, iOS, and the TypeScript boundary.
required = {
    "getCapabilities", "createTerminal", "destroyTerminal", "writeData",
    "scrollViewport", "scrollViewportToBottom", "resize", "setTheme",
    "encodeMouseEvent", "getRenderSnapshot", "getVisibleText", "getVisibleHtml",
    "getCrashBreadcrumb", "clearCrashBreadcrumb",
}
ts_methods = set(re.findall(r"^\s{2}([A-Za-z][A-Za-z0-9]+)\??\(", typescript, re.MULTILINE))
ts_methods |= set(re.findall(r"^\s{2}([A-Za-z][A-Za-z0-9]+)\??:\s*\(", typescript, re.MULTILINE))
android_methods = set(re.findall(r'Function\("([A-Za-z][A-Za-z0-9]+)"\)', android))
swift_methods = set(re.findall(r'Function\("([A-Za-z][A-Za-z0-9]+)"\)', swift))
for surface, methods in (("TypeScript", ts_methods), ("Android", android_methods), ("iOS", swift_methods)):
    missing = sorted(required - methods)
    if missing:
        fail(f"{surface} terminal surface missing: {', '.join(missing)}")

# Parameter order is part of the synchronous Expo contract. These fragments
# deliberately mirror Kotlin and TypeScript instead of attempting to compile Swift.
swift_signatures = {
    "createTerminal": "(cols: Int, rows: Int) throws -> Int",
    "destroyTerminal": "(handle: Int) in",
    "writeData": "(handle: Int, data: String) throws in",
    "scrollViewport": "(handle: Int, delta: Int) throws in",
    "scrollViewportToBottom": "(handle: Int) throws in",
    "resize": "(handle: Int, cols: Int, rows: Int, cellWidth: Double, cellHeight: Double) throws in",
    "setTheme": "(handle: Int, foreground: String, background: String, cursor: String, palette: [String]) throws in",
    "encodeMouseEvent": "(handle: Int, action: Int, button: Int, x: Double, y: Double, mods: Int, anyButtonPressed: Bool) throws -> String",
    "getRenderSnapshot": "(handle: Int) throws -> [String: Any]",
    "getVisibleText": "(handle: Int) throws -> String",
    "getVisibleHtml": "(handle: Int) throws -> String",
}
for method, signature in swift_signatures.items():
    if f'Function("{method}") {{ {signature}' not in swift:
        fail(f"iOS Expo signature drift for {method}: expected {signature}")

android_signatures = {
    "createTerminal": "{ cols: Int, rows: Int ->",
    "destroyTerminal": "{ handleId: Int ->",
    "writeData": "{ handleId: Int, data: String ->",
    "resize": "{ handleId: Int, cols: Int, rows: Int, cellWidth: Float, cellHeight: Float ->",
    "setTheme": "{ handleId: Int, foreground: String, background: String, cursor: String, palette: List<String> ->",
    "encodeMouseEvent": "{ handleId: Int, action: Int, button: Int, x: Float, y: Float, mods: Int, anyButtonPressed: Boolean ->",
}
for method, signature in android_signatures.items():
    if f'Function("{method}") {signature}' not in android:
        fail(f"Android reference signature changed for {method}")

# Render result names and types are kept identical to the existing WebView API.
for field in ("dirty", "rows", "cols", "html", "cursorCol", "cursorRow", "cursorVisible"):
    if f'@"{field}"' not in bridge:
        fail(f"iOS render snapshot missing field: {field}")
for dirty in ("none", "partial", "full"):
    if f'@"{dirty}"' not in bridge or f"'{dirty}'" not in typescript:
        fail(f"render dirty value mismatch: {dirty}")
for capability in ("nativeBridge", "vtCore", "renderer", "reason"):
    if f'"{capability}"' not in swift or capability not in typescript:
        fail(f"capability result shape mismatch: {capability}")

if "vendored_frameworks = '../libs/apple/ghostty-vt.xcframework'" not in podspec:
    fail("podspec must mandatory-link the pinned XCFramework")
if "s.platforms      = { :ios => '17.0' }" not in podspec:
    fail("podspec deployment target must match the pinned XCFramework")
if "./plugins/withZenIOSDeploymentTarget" not in app_config or "MINIMUM_IOS = '17.0'" not in deployment_plugin:
    fail("generated iOS app target must match the pinned XCFramework deployment target")
if '"vtCore": false' not in swift or '"renderer": "none"' not in swift:
    fail("iOS capability must remain gated until macOS compile/link acceptance")
if "removeObjectForKey:@(handle)" not in bridge:
    fail("destroy must remove ownership before freeing")
if "for (NSValue *value in _terminals.allValues)" not in bridge:
    fail("module owner must free remaining terminals during teardown")

print(f"ok: {len(symbols)} pinned C ABI symbols/types/enums")
print(f"ok: {len(selectors)} explicit ObjC/Swift selector mappings")
print(f"ok: {len(required)} JS methods across TypeScript, Android, and iOS")
print("ok: mandatory XCFramework linkage, iOS 17 floor, gated capability, ownership invariants")
