import ExpoModulesCore
import Foundation
import UIKit

private final class UnknownTerminalHandleException: GenericException<Int>, @unchecked Sendable {
  override var reason: String {
    "Unknown terminal handle id: \(param)"
  }
}

private final class TerminalCreationException: Exception, @unchecked Sendable {
  override var reason: String {
    "libghostty-vt failed to create a terminal"
  }
}

public final class ZenTerminalVtModule: Module {
  private let lock = NSLock()
  private var terminalHandles: [Int: UInt64] = [:]
  private var nextHandleID = 1

  public func definition() -> ModuleDefinition {
    Name("ZenTerminalVt")

    Function("createTerminal") { (cols: Int, rows: Int) throws -> Int in
      try self.withPersistentBreadcrumb(
        operation: "createTerminal",
        detail: "cols=\(cols) rows=\(rows)"
      ) {
        let nativeHandle = ZenTerminalVtBridge.createTerminal(withCols: cols, rows: rows)
        guard nativeHandle != 0 else {
          throw TerminalCreationException()
        }
        return self.store(nativeHandle: nativeHandle)
      }
    }

    Function("destroyTerminal") { (handleID: Int) throws in
      try self.withPersistentBreadcrumb(
        operation: "destroyTerminal",
        detail: "handleId=\(handleID)"
      ) {
        guard let nativeHandle = self.remove(handleID: handleID) else {
          return
        }
        ZenTerminalVtBridge.destroyTerminal(nativeHandle)
      }
    }

    Function("writeData") { (handleID: Int, data: String) throws in
      try self.withNativeHandle(handleID) { nativeHandle in
        ZenTerminalVtBridge.writeData(data, toTerminal: nativeHandle)
      }
    }

    Function("scrollViewport") { (handleID: Int, delta: Int) throws in
      try self.withNativeHandle(handleID) { nativeHandle in
        ZenTerminalVtBridge.scrollTerminal(nativeHandle, byLines: delta)
      }
    }

    Function("scrollViewportToBottom") { (handleID: Int) throws in
      try self.withNativeHandle(handleID) { nativeHandle in
        ZenTerminalVtBridge.scrollTerminal(toBottom: nativeHandle)
      }
    }

    Function("resize") {
      (handleID: Int, cols: Int, rows: Int, cellWidth: Double, cellHeight: Double) throws in
      try self.withPersistentBreadcrumb(
        operation: "resize",
        detail: "handleId=\(handleID) cols=\(cols) rows=\(rows) cellWidth=\(cellWidth) cellHeight=\(cellHeight)"
      ) {
        try self.withNativeHandle(handleID) { nativeHandle in
          ZenTerminalVtBridge.resizeTerminal(
            nativeHandle,
            cols: cols,
            rows: rows,
            cellWidth: cellWidth,
            cellHeight: cellHeight
          )
        }
      }
    }

    Function("setTheme") {
      (handleID: Int, foreground: String, background: String, cursor: String, palette: [String]) throws in
      try self.withPersistentBreadcrumb(
        operation: "setTheme",
        detail: "handleId=\(handleID) paletteSize=\(palette.count)"
      ) {
        try self.withNativeHandle(handleID) { nativeHandle in
          _ = ZenTerminalVtBridge.setThemeForTerminal(
            nativeHandle,
            foreground: foreground,
            background: background,
            cursor: cursor,
            palette: palette
          )
        }
      }
    }

    Function("encodeMouseEvent") {
      (handleID: Int, action: Int, button: Int, x: Double, y: Double, mods: Int, anyButtonPressed: Bool) throws -> String in
      try self.withNativeHandle(handleID) { nativeHandle in
        ZenTerminalVtBridge.encodeMouse(
          forTerminal: nativeHandle,
          action: action,
          button: button,
          x: x,
          y: y,
          mods: mods,
          anyButtonPressed: anyButtonPressed
        )
      }
    }

    Function("getRenderSnapshot") { (handleID: Int) throws -> [String: Any] in
      try self.withNativeHandle(handleID) { nativeHandle in
        ZenTerminalVtBridge.renderSnapshot(forTerminal: nativeHandle) as [String: Any]
      }
    }

    Function("getVisibleText") { (handleID: Int) throws -> String in
      try self.withNativeHandle(handleID) { nativeHandle in
        ZenTerminalVtBridge.visibleText(forTerminal: nativeHandle)
      }
    }

    Function("getVisibleHtml") { (handleID: Int) throws -> String in
      try self.withNativeHandle(handleID) { nativeHandle in
        ZenTerminalVtBridge.visibleHTML(forTerminal: nativeHandle)
      }
    }

    Function("getCrashBreadcrumb") { () -> [String: Any]? in
      self.getBreadcrumb()
    }

    Function("clearCrashBreadcrumb") {
      self.clearBreadcrumb()
    }

    OnDestroy {
      self.destroyAllTerminals()
    }
  }

  private func store(nativeHandle: UInt64) -> Int {
    lock.lock()
    defer { lock.unlock() }

    while terminalHandles[nextHandleID] != nil || nextHandleID == 0 {
      nextHandleID = nextHandleID == Int.max ? 1 : nextHandleID + 1
    }

    let handleID = nextHandleID
    terminalHandles[handleID] = nativeHandle
    nextHandleID = nextHandleID == Int.max ? 1 : nextHandleID + 1
    return handleID
  }

  private func remove(handleID: Int) -> UInt64? {
    lock.lock()
    defer { lock.unlock() }
    return terminalHandles.removeValue(forKey: handleID)
  }

  private func withNativeHandle<T>(_ handleID: Int, body: (UInt64) throws -> T) throws -> T {
    lock.lock()
    defer { lock.unlock() }

    guard let nativeHandle = terminalHandles[handleID] else {
      throw UnknownTerminalHandleException(handleID)
    }
    return try body(nativeHandle)
  }

  private func destroyAllTerminals() {
    lock.lock()
    let nativeHandles = Array(terminalHandles.values)
    terminalHandles.removeAll()
    lock.unlock()

    for nativeHandle in nativeHandles {
      ZenTerminalVtBridge.destroyTerminal(nativeHandle)
    }
  }

  private func withPersistentBreadcrumb<T>(
    operation: String,
    detail: String,
    body: () throws -> T
  ) throws -> T {
    setBreadcrumb(stage: "before", operation: operation, detail: detail)
    let result = try body()
    setBreadcrumb(stage: "after", operation: operation, detail: detail)
    return result
  }

  private func setBreadcrumb(stage: String, operation: String, detail: String) {
    let defaults = UserDefaults.standard
    defaults.set(stage, forKey: "zen_terminal_vt.stage")
    defaults.set(operation, forKey: "zen_terminal_vt.operation")
    defaults.set(detail, forKey: "zen_terminal_vt.detail")
    defaults.set(Date().timeIntervalSince1970 * 1_000, forKey: "zen_terminal_vt.timestamp_ms")
    defaults.synchronize()
  }

  private func getBreadcrumb() -> [String: Any]? {
    let defaults = UserDefaults.standard
    guard let stage = defaults.string(forKey: "zen_terminal_vt.stage") else {
      return nil
    }

    let version = ProcessInfo.processInfo.operatingSystemVersion
    return [
      "stage": stage,
      "operation": defaults.string(forKey: "zen_terminal_vt.operation") ?? "",
      "detail": defaults.string(forKey: "zen_terminal_vt.detail") ?? "",
      "timestampMs": defaults.double(forKey: "zen_terminal_vt.timestamp_ms"),
      "abi": Self.architecture,
      "model": UIDevice.current.model,
      "brand": "Apple",
      "sdkInt": version.majorVersion,
    ]
  }

  private func clearBreadcrumb() {
    let defaults = UserDefaults.standard
    for key in ["stage", "operation", "detail", "timestamp_ms"] {
      defaults.removeObject(forKey: "zen_terminal_vt.\(key)")
    }
    defaults.synchronize()
  }

  private static var architecture: String {
    #if arch(arm64)
    return "arm64"
    #elseif arch(x86_64)
    return "x86_64"
    #else
    return "unknown"
    #endif
  }
}
