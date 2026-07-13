import ExpoModulesCore

/// Expo surface for the mandatory pinned Ghostty XCFramework and ObjC++ owner.
/// All operations are synchronous to preserve terminal mutation/snapshot ordering.
public final class ZenTerminalVtModule: Module {
  private let bridge = ZenTerminalVtBridge()

  private func value<T>(_ result: [String: Any], as type: T.Type = T.self) throws -> T {
    guard result["ok"] as? Bool == true else {
      throw GenericException(result["error"] as? String ?? "Unknown iOS terminal bridge error")
    }
    guard let value = result["value"] as? T else {
      throw GenericException("iOS terminal bridge returned an invalid result")
    }
    return value
  }

  private func success(_ result: [String: Any]) throws {
    guard result["ok"] as? Bool == true else {
      throw GenericException(result["error"] as? String ?? "Unknown iOS terminal bridge error")
    }
  }

  public func definition() -> ModuleDefinition {
    Name("ZenTerminalVt")

    Function("getCapabilities") {
      return [
        "nativeBridge": true,
        "vtCore": false,
        "renderer": "none",
        "reason": "The iOS Ghostty bridge is implemented and mandatory-linked, but this source revision has not passed the required macOS compile/link and device acceptance.",
      ] as [String: Any]
    }

    Function("createTerminal") { (cols: Int, rows: Int) throws -> Int in
      let handle: NSNumber = try self.value(
        self.bridge.createTerminal(columns: cols, rows: rows)
      )
      return handle.intValue
    }

    Function("destroyTerminal") { (handle: Int) in
      // Explicitly idempotent: the owner removes before freeing and ignores repeats.
      self.bridge.destroyTerminal(handle: handle)
    }

    Function("writeData") { (handle: Int, data: String) throws in
      try self.success(self.bridge.writeData(data, handle: handle))
    }

    Function("scrollViewport") { (handle: Int, delta: Int) throws in
      try self.success(self.bridge.scrollViewport(delta, handle: handle))
    }

    Function("scrollViewportToBottom") { (handle: Int) throws in
      try self.success(self.bridge.scrollViewportToBottom(handle: handle))
    }

    Function("resize") { (handle: Int, cols: Int, rows: Int, cellWidth: Double, cellHeight: Double) throws in
      try self.success(self.bridge.resize(
        handle: handle,
        columns: cols,
        rows: rows,
        cellWidth: cellWidth,
        cellHeight: cellHeight
      ))
    }

    Function("setTheme") { (handle: Int, foreground: String, background: String, cursor: String, palette: [String]) throws in
      try self.success(self.bridge.setTheme(
        handle: handle,
        foreground: foreground,
        background: background,
        cursor: cursor,
        palette: palette
      ))
    }

    Function("encodeMouseEvent") { (handle: Int, action: Int, button: Int, x: Double, y: Double, mods: Int, anyButtonPressed: Bool) throws -> String in
      return try self.value(self.bridge.encodeMouseEvent(
        handle: handle,
        action: action,
        button: button,
        x: x,
        y: y,
        mods: mods,
        anyButtonPressed: anyButtonPressed
      ))
    }

    Function("getRenderSnapshot") { (handle: Int) throws -> [String: Any] in
      return try self.value(self.bridge.renderSnapshot(handle: handle))
    }

    Function("getVisibleText") { (handle: Int) throws -> String in
      return try self.value(self.bridge.visibleText(handle: handle))
    }

    Function("getVisibleHtml") { (handle: Int) throws -> String in
      return try self.value(self.bridge.visibleHTML(handle: handle))
    }

    // Android-only persistent crash diagnostics remain callable with neutral values.
    Function("getCrashBreadcrumb") {
      return nil as [String: Any]?
    }

    Function("clearCrashBreadcrumb") {}
  }
}
