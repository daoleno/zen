import Foundation
import Network

enum DesktopTransportPolicy {
  private static func privateAddress(_ bytes: [UInt8]) -> Bool {
    if bytes.count == 16 && bytes.prefix(10).allSatisfy({ $0 == 0 }) && bytes[10] == 255 && bytes[11] == 255 {
      return privateAddress(Array(bytes.suffix(4)))
    }
    if bytes.count == 4 {
      return bytes[0] == 10 || (bytes[0] == 172 && (16...31).contains(bytes[1])) ||
        (bytes[0] == 192 && bytes[1] == 168) || (bytes[0] == 100 && (64...127).contains(bytes[1]))
    }
    return bytes.count == 16 && (bytes[0] & 254) == 252
  }

  private static func parse(_ value: String, endpoint: Bool) -> URLComponents? {
    guard let parsed = URLComponents(string: value), ["ws", "wss"].contains(parsed.scheme ?? ""),
      parsed.host != nil, parsed.user == nil, parsed.password == nil,
      parsed.query == nil, parsed.fragment == nil,
      (endpoint ? parsed.percentEncodedPath == "/desktop" : ["", "/"].contains(parsed.percentEncodedPath)),
      parsed.port == nil || (1...65535).contains(parsed.port!) else { return nil }
    return parsed
  }

  private static func sameOrigin(_ a: URLComponents, _ b: URLComponents) -> Bool {
    a.scheme == b.scheme && a.host?.lowercased() == b.host?.lowercased() &&
      (a.port ?? (a.scheme == "wss" ? 443 : 80)) == (b.port ?? (b.scheme == "wss" ? 443 : 80))
  }

  static func allows(_ config: [String: String]) -> Bool {
    guard let target = parse(config["url"] ?? "", endpoint: true),
      let bound = parse(config["boundOrigin"] ?? "", endpoint: false),
      let source = parse(config["sourceOrigin"] ?? "", endpoint: false), sameOrigin(target, bound) else { return false }
    switch config["transport"] {
    case "tls": return target.scheme == "wss" && sameOrigin(target, source)
    case "pinned-link":
      let pin = config["transportPin"] ?? ""
      return target.scheme == "ws" && target.host == "127.0.0.1" && source.scheme == "wss" &&
        pin.utf8.count == 64 && pin.utf8.allSatisfy { (48...57).contains($0) || (65...70).contains($0) || (97...102).contains($0) }
    case "trusted-lan":
      guard target.scheme == "ws", sameOrigin(target, source), let rawHost = target.host, !rawHost.contains("%") else { return false }
      let host = rawHost.hasPrefix("[") && rawHost.hasSuffix("]") ? String(rawHost.dropFirst().dropLast()) : rawHost
      if let address = IPv4Address(host) { return privateAddress(Array(address.rawValue)) }
      if let address = IPv6Address(host) { return privateAddress(Array(address.rawValue)) }
      return false
    default: return false
    }
  }
}

final class DesktopNoRedirectDelegate: NSObject, URLSessionTaskDelegate {
  func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                  newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) {
    completionHandler(nil)
  }
}
