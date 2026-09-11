import Foundation

public enum PinnedEndpointRegistry {
  private static let lock = NSLock()
  private static var entries: [Int: (UUID, Data)] = [:]

  static func register(port: Int, pin: Data, owner: UUID) {
    lock.lock(); defer { lock.unlock() }
    entries[port] = (owner, pin)
  }

  static func remove(port: Int, owner: UUID) {
    lock.lock(); defer { lock.unlock() }
    if entries[port]?.0 == owner { entries.removeValue(forKey: port) }
  }

  public static func contains(port: Int, pin: String) -> Bool {
    lock.lock(); defer { lock.unlock() }
    guard let entry = entries[port] else { return false }
    return entry.1.map { String(format: "%02x", $0) }.joined() == pin.lowercased()
  }
}
