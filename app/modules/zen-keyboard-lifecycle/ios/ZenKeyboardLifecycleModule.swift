import ExpoModulesCore
import React
import UIKit

public final class ZenKeyboardLifecycleModule: Module {
  public func definition() -> ModuleDefinition {
    Name("ZenKeyboardLifecycle")

    AsyncFunction("getForegroundSnapshot") { (composerNativeId: String, revision: Int) -> [String: Any] in
      guard let window = Self.foregroundWindow() else {
        return Self.closedSnapshot(revision: revision, evidence: "window_unavailable")
      }

      let keyboardFrame = window.keyboardLayoutGuide.layoutFrame
      let occludedFrame = window.bounds.intersection(keyboardFrame)
      let imeHeight = occludedFrame.isNull ? 0 : max(0, occludedFrame.height)
      let imeVisible = imeHeight > 0 && keyboardFrame.minY < window.bounds.maxY
      let composer = Self.findView(nativeId: composerNativeId, in: window)
      let responder = Self.findFirstResponder(in: window)
      let composerFocused = composer.map { responder?.isDescendant(of: $0) == true } ?? false

      return [
        "revision": revision,
        "imeVisible": imeVisible,
        "imeHeight": imeVisible ? imeHeight : 0,
        "composerFocused": composerFocused,
        "evidence": "keyboard_layout_guide"
      ]
    }.runOnQueue(.main)
  }

  private static func foregroundWindow() -> UIWindow? {
    let foregroundScenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
      .filter { $0.activationState == .foregroundActive }
    return foregroundScenes.flatMap(\.windows).first(where: \.isKeyWindow)
      ?? foregroundScenes.flatMap(\.windows).first
  }

  private static func findFirstResponder(in view: UIView) -> UIView? {
    if view.isFirstResponder { return view }
    for child in view.subviews {
      if let responder = findFirstResponder(in: child) { return responder }
    }
    return nil
  }

  private static func findView(nativeId: String, in view: UIView) -> UIView? {
    if view.nativeIdentifier == nativeId { return view }
    for child in view.subviews {
      if let match = findView(nativeId: nativeId, in: child) { return match }
    }
    return nil
  }

  private static func closedSnapshot(revision: Int, evidence: String) -> [String: Any] {
    return [
      "revision": revision,
      "imeVisible": false,
      "imeHeight": 0,
      "composerFocused": false,
      "evidence": evidence
    ]
  }
}

private extension UIView {
  var nativeIdentifier: String? {
    if responds(to: NSSelectorFromString("nativeId")) {
      return value(forKey: "nativeId") as? String
    }
    return nativeID
  }
}
