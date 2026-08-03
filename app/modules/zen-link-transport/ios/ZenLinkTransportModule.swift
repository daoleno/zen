import CryptoKit
import ExpoModulesCore
import Foundation
import Network
import Security

public final class ZenLinkTransportModule: Module {
  private let lock = NSLock()
  private var proxies: [String: PinnedProxy] = [:]

  public func definition() -> ModuleDefinition {
    Name("ZenLinkTransport")

    AsyncFunction("start") {
      (key: String, host: String, port: Int, pin: String, mode: String, promise: Promise) in
      do {
        try validate(key: key, host: host, port: port, pin: pin, mode: mode)
      } catch {
        promise.reject(error)
        return
      }

      self.lock.lock()
      if let existing = self.proxies[key],
         existing.matches(host: host, port: port, pin: pin, mode: mode) {
        let response = ["port": existing.localPort, "rttMs": existing.lastRTTMilliseconds]
        self.lock.unlock()
        promise.resolve(response)
        return
      }
      let previous = self.proxies.removeValue(forKey: key)
      self.lock.unlock()
      previous?.stop()

      guard let expectedPin = Data(hex: pin) else {
        promise.reject(LinkTransportError.invalidPin)
        return
      }
      let proxy = PinnedProxy(
        host: host,
        port: UInt16(port),
        pin: expectedPin,
        measureBeforeListen: mode == "measure"
      )
      proxy.start { result in
        switch result {
        case .success(let response):
          self.lock.lock()
          self.proxies[key] = proxy
          self.lock.unlock()
          promise.resolve(["port": response.port, "rttMs": response.rttMilliseconds])
        case .failure(let error):
          proxy.stop()
          promise.reject(error)
        }
      }
    }

    AsyncFunction("stop") { (key: String) in
      self.lock.lock()
      let proxy = self.proxies.removeValue(forKey: key)
      self.lock.unlock()
      proxy?.stop()
    }

    AsyncFunction("stopAll") {
      self.stopAll()
    }

    OnDestroy {
      self.stopAll()
    }
  }

  private func stopAll() {
    lock.lock()
    let current = Array(proxies.values)
    proxies.removeAll()
    lock.unlock()
    current.forEach { $0.stop() }
  }
}

private enum LinkTransportError: Error, LocalizedError {
  case invalidInput(String)
  case invalidPin
  case unavailable
  case pinMismatch

  var errorDescription: String? {
    switch self {
    case .invalidInput(let detail): return detail
    case .invalidPin: return "Zen Link SPKI pin is invalid."
    case .unavailable: return "Zen Link could not reach a pinned relay candidate."
    case .pinMismatch: return "Zen Link transport certificate pin mismatch."
    }
  }
}

private func validate(
  key: String,
  host: String,
  port: Int,
  pin: String,
  mode: String
) throws {
  guard !key.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
    throw LinkTransportError.invalidInput("Zen Link tunnel key is required.")
  }
  guard !host.isEmpty, !host.contains("/"), !host.contains("\0") else {
    throw LinkTransportError.invalidInput("Zen Link host is invalid.")
  }
  guard (1...65535).contains(port) else {
    throw LinkTransportError.invalidInput("Zen Link port is invalid.")
  }
  guard pin.range(of: "^[0-9a-fA-F]{64}$", options: .regularExpression) != nil else {
    throw LinkTransportError.invalidPin
  }
  guard mode == "measure" || mode == "on-demand" else {
    throw LinkTransportError.invalidInput("Zen Link tunnel mode is invalid.")
  }
}

private final class PinnedProxy {
  private struct StartResult {
    let port: Int
    let rttMilliseconds: Int
  }

  private let host: String
  private let port: UInt16
  private let pin: Data
  private let measureBeforeListen: Bool
  private let queue = DispatchQueue(label: "dev.zen.link.transport")
  private let lock = NSLock()
  private var listener: NWListener?
  private var connections: [ObjectIdentifier: Bridge] = [:]
  private var stopped = false
  private var startCompleted = false

  private(set) var localPort = 0
  private(set) var lastRTTMilliseconds = 0

  init(host: String, port: UInt16, pin: Data, measureBeforeListen: Bool) {
    self.host = host
    self.port = port
    self.pin = pin
    self.measureBeforeListen = measureBeforeListen
  }

  func matches(host: String, port: Int, pin: String, mode: String) -> Bool {
    guard let decoded = Data(hex: pin) else { return false }
    lock.lock()
    defer { lock.unlock() }
    return !stopped &&
      self.host == host &&
      self.port == UInt16(port) &&
      self.pin == decoded &&
      self.measureBeforeListen == (mode == "measure")
  }

  func start(completion: @escaping (Result<StartResult, Error>) -> Void) {
    if !measureBeforeListen {
      startListener(rttMilliseconds: 0, completion: completion)
      return
    }
    probe { result in
      switch result {
      case .failure(let error):
        self.finishStart(.failure(error), completion: completion)
      case .success(let rtt):
        self.startListener(rttMilliseconds: rtt, completion: completion)
      }
    }
  }

  private func startListener(
    rttMilliseconds: Int,
    completion: @escaping (Result<StartResult, Error>) -> Void
  ) {
    do {
      let parameters = NWParameters.tcp
      parameters.requiredLocalEndpoint = .hostPort(
        host: NWEndpoint.Host("127.0.0.1"),
        port: .any
      )
      let listener = try NWListener(using: parameters, on: .any)
      self.listener = listener
      listener.newConnectionHandler = { [weak self] local in
        self?.accept(local: local)
      }
      listener.stateUpdateHandler = { [weak self] state in
        guard let self else { return }
        switch state {
        case .ready:
          guard let localPort = listener.port else {
            self.finishStart(
              .failure(LinkTransportError.unavailable),
              completion: completion
            )
            return
          }
          self.lock.lock()
          self.localPort = Int(localPort.rawValue)
          self.lastRTTMilliseconds = rttMilliseconds
          self.lock.unlock()
          self.finishStart(
            .success(StartResult(
              port: Int(localPort.rawValue),
              rttMilliseconds: rttMilliseconds
            )),
            completion: completion
          )
        case .failed(let error):
          self.finishStart(.failure(error), completion: completion)
        default:
          break
        }
      }
      listener.start(queue: self.queue)
    } catch {
      self.finishStart(.failure(error), completion: completion)
    }
  }

  private func finishStart(
    _ result: Result<StartResult, Error>,
    completion: @escaping (Result<StartResult, Error>) -> Void
  ) {
    lock.lock()
    if startCompleted {
      lock.unlock()
      return
    }
    startCompleted = true
    lock.unlock()
    completion(result)
  }

  func stop() {
    lock.lock()
    if stopped {
      lock.unlock()
      return
    }
    stopped = true
    let currentListener = listener
    let currentConnections = Array(connections.values)
    connections.removeAll()
    lock.unlock()
    currentListener?.cancel()
    currentConnections.forEach { $0.stop() }
  }

  private func probe(completion: @escaping (Result<Int, Error>) -> Void) {
    let started = DispatchTime.now().uptimeNanoseconds
    let connection = makeRemoteConnection()
    var completed = false
    connection.stateUpdateHandler = { state in
      switch state {
      case .ready:
        guard !completed else { return }
        completed = true
        let elapsed = DispatchTime.now().uptimeNanoseconds - started
        connection.cancel()
        completion(.success(max(1, Int(elapsed / 1_000_000))))
      case .failed(let error):
        guard !completed else { return }
        completed = true
        connection.cancel()
        completion(.failure(error))
      default:
        break
      }
    }
    connection.start(queue: queue)
    queue.asyncAfter(deadline: .now() + 5) {
      guard !completed else { return }
      completed = true
      connection.cancel()
      completion(.failure(LinkTransportError.unavailable))
    }
  }

  private func accept(local: NWConnection) {
    lock.lock()
    let shouldReject = stopped || connections.count >= 64
    lock.unlock()
    if shouldReject {
      local.cancel()
      return
    }

    let bridge = Bridge(local: local, remote: makeRemoteConnection()) { [weak self] identifier in
      self?.lock.lock()
      self?.connections.removeValue(forKey: identifier)
      self?.lock.unlock()
    }
    let identifier = ObjectIdentifier(bridge)
    lock.lock()
    connections[identifier] = bridge
    lock.unlock()
    bridge.start(queue: queue)
  }

  private func makeRemoteConnection() -> NWConnection {
    let tls = NWProtocolTLS.Options()
    sec_protocol_options_set_min_tls_protocol_version(
      tls.securityProtocolOptions,
      .TLSv13
    )
    sec_protocol_options_set_tls_server_name(tls.securityProtocolOptions, host)
    let expectedPin = pin
    sec_protocol_options_set_verify_block(
      tls.securityProtocolOptions,
      { _, secTrust, complete in
        let trust = sec_trust_copy_ref(secTrust).takeRetainedValue()
        guard
          SecTrustGetCertificateCount(trust) > 0,
          let certificate = SecTrustGetCertificateAtIndex(trust, 0),
          let key = SecCertificateCopyKey(certificate),
          let rawKey = SecKeyCopyExternalRepresentation(key, nil) as Data?
        else {
          complete(false)
          return
        }
        // RFC 8410 SubjectPublicKeyInfo prefix for a 32-byte Ed25519 key.
        let prefix = Data([0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00])
        let spki = prefix + rawKey
        let actualPin = Data(SHA256.hash(data: spki))
        complete(actualPin == expectedPin)
      },
      queue
    )
    let parameters = NWParameters(tls: tls, tcp: NWProtocolTCP.Options())
    return NWConnection(
      host: NWEndpoint.Host(host),
      port: NWEndpoint.Port(rawValue: port)!,
      using: parameters
    )
  }
}

private final class Bridge {
  private let local: NWConnection
  private let remote: NWConnection
  private let finished: (ObjectIdentifier) -> Void
  private let lock = NSLock()
  private var directionsFinished = 0
  private var stopped = false

  init(
    local: NWConnection,
    remote: NWConnection,
    finished: @escaping (ObjectIdentifier) -> Void
  ) {
    self.local = local
    self.remote = remote
    self.finished = finished
  }

  func start(queue: DispatchQueue) {
    local.start(queue: queue)
    remote.stateUpdateHandler = { [weak self] state in
      guard let self else { return }
      switch state {
      case .ready:
        self.pump(from: self.local, to: self.remote)
        self.pump(from: self.remote, to: self.local)
      case .failed, .cancelled:
        self.stop()
      default:
        break
      }
    }
    remote.start(queue: queue)
  }

  func stop() {
    lock.lock()
    if stopped {
      lock.unlock()
      return
    }
    stopped = true
    lock.unlock()
    local.cancel()
    remote.cancel()
    finished(ObjectIdentifier(self))
  }

  private func pump(from source: NWConnection, to destination: NWConnection) {
    source.receive(minimumIncompleteLength: 1, maximumLength: 32 * 1024) {
      [weak self] data, _, complete, error in
      guard let self else { return }
      if let error {
        _ = error
        self.stop()
        return
      }
      destination.send(
        content: data,
        isComplete: complete,
        completion: .contentProcessed { sendError in
          if sendError != nil {
            self.stop()
          } else if complete {
            self.directionFinished()
          } else {
            self.pump(from: source, to: destination)
          }
        }
      )
    }
  }

  private func directionFinished() {
    lock.lock()
    directionsFinished += 1
    let done = directionsFinished == 2
    lock.unlock()
    if done {
      stop()
    }
  }
}

private extension Data {
  init?(hex: String) {
    let normalized = hex.trimmingCharacters(in: .whitespacesAndNewlines)
    guard normalized.count % 2 == 0 else { return nil }
    var output = Data(capacity: normalized.count / 2)
    var index = normalized.startIndex
    while index < normalized.endIndex {
      let next = normalized.index(index, offsetBy: 2)
      guard let byte = UInt8(normalized[index..<next], radix: 16) else {
        return nil
      }
      output.append(byte)
      index = next
    }
    self = output
  }
}
