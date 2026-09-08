import ExpoModulesCore
import AVFoundation
import CoreMedia
import UIKit

public final class ZenRemoteDesktopModule: Module {
  public func definition() -> ModuleDefinition {
    Name("ZenRemoteDesktop")
    View(DesktopView.self) {
      Events("onState")
      Prop("connection") { (view: DesktopView, value: String) in view.connect(value) }
      AsyncFunction("sendCommand") { (view: DesktopView, generation: String, sequence: Int, value: String) -> Bool in
        view.sendCommand(generation, sequence, value)
      }
      AsyncFunction("disconnect") { (view: DesktopView, generation: String) in view.disconnect(generation) }
      OnViewDestroys { (view: DesktopView) in view.stop() }
    }
  }
}

final class DesktopView: ExpoView {
  let onState = EventDispatcher()
  private let video = AVSampleBufferDisplayLayer()
  private var task: URLSessionWebSocketTask?
  private var session: URLSession?
  private var heartbeat: Timer?
  private var epoch = 0
  private var connection = ""
  private var inputGeneration = ""
  private var inputSequence = 0
  private var selectedSource = ""
  private var format: CMVideoFormatDescription?
  private var needIDR = true
  private var submitted = 0
  private var dropped = 0
  private var pendingSends = 0
  private var lastVideo: TimeInterval?
  private var terminalState = false
  private var width = 1280
  private var height = 720
  private var observer: NSObjectProtocol?
  private var displayObserver: NSKeyValueObservation?

  required init(appContext: AppContext? = nil) {
    super.init(appContext: appContext)
    backgroundColor = .black
    video.videoGravity = .resizeAspect
    layer.addSublayer(video)
    observer = NotificationCenter.default.addObserver(forName: UIApplication.willResignActiveNotification, object: nil, queue: .main) { [weak self] _ in
      self?.stop()
      self?.state("disconnected")
    }
    displayObserver = video.observe(\.isReadyForDisplay, options: [.new]) { [weak self] layer, _ in
      DispatchQueue.main.async {
        guard let self, self.task != nil, layer.isReadyForDisplay else { return }
        self.state("connected")
      }
    }
  }

  deinit {
    if let observer { NotificationCenter.default.removeObserver(observer) }
    heartbeat?.invalidate()
    task?.cancel(with: .goingAway, reason: nil)
    session?.invalidateAndCancel()
  }

  override func layoutSubviews() {
    super.layoutSubviews()
    CATransaction.begin()
    CATransaction.setDisableActions(true)
    video.frame = bounds
    CATransaction.commit()
  }

  private func state(_ value: String, _ reason: String = "") {
    onState(["state": value, "reason": reason, "source": selectedSource, "width": width, "height": height,
             "submitted": submitted, "dropped": dropped])
  }

  func connect(_ value: String) {
    guard value != connection else { return }
    stop()
    terminalState = false
    connection = value
    guard !value.isEmpty else { return }
    guard let bytes = value.data(using: .utf8),
      let config = try? JSONSerialization.jsonObject(with: bytes) as? [String: String],
      let rawURL = config["url"], let url = URL(string: rawURL),
      DesktopTransportPolicy.allows(config),
      let authorization = config["authorization"],
      let inputGeneration = config["inputGeneration"], !inputGeneration.isEmpty else {
      state("disconnected", "Desktop transport approval is missing or invalid."); return
    }
    var request = URLRequest(url: url)
    self.inputGeneration = inputGeneration
    request.setValue(authorization, forHTTPHeaderField: "Authorization")
    request.timeoutInterval = 10
    let ownedSession = URLSession(configuration: .ephemeral, delegate: DesktopNoRedirectDelegate(), delegateQueue: nil)
    session = ownedSession
    let socket = ownedSession.webSocketTask(with: request)
    socket.maximumMessageSize = 4 * 1024 * 1024
    task = socket
    socket.resume()
    let generation = epoch
    receive(socket, generation)
    heartbeat = Timer.scheduledTimer(withTimeInterval: 3, repeats: true) { [weak self] _ in
      guard let self else { return }
      if let last = self.lastVideo, ProcessInfo.processInfo.systemUptime - last > 10 {
        self.stop(); self.state("disconnected", "Desktop video timed out."); return
      }
      self.send("{\"type\":\"ping\"}")
    }
  }

  private func receive(_ socket: URLSessionWebSocketTask, _ generation: Int) {
    socket.receive { [weak self] result in
      DispatchQueue.main.async {
        guard let self, generation == self.epoch else { return }
        do {
          switch try result.get() {
          case .string(let text):
            guard text.utf8.count <= 8192, let data = text.data(using: .utf8),
              let status = try JSONSerialization.jsonObject(with: data) as? [String: Any],
              let state = status["state"] as? String,
              ["sources", "requesting", "streaming", "denied", "unsupported", "disconnected"].contains(state)
              else { throw DesktopError.invalidFrame }
            if state == "sources" {
              guard let sources = status["sources"] as? [[String: Any]], sources.count == 1,
                let source = sources[0]["id"] as? String, ["x11", "wayland"].contains(source) else { throw DesktopError.invalidFrame }
              self.selectedSource = source
            }
            guard (status["width"] == nil) == (status["height"] == nil) else { throw DesktopError.invalidFrame }
            if status["width"] != nil {
              guard let width = status["width"] as? Int, let height = status["height"] as? Int else { throw DesktopError.invalidFrame }
              guard (2...4096).contains(width), (2...4096).contains(height) else { throw DesktopError.invalidFrame }
              self.width = width; self.height = height
            }
            self.terminalState = ["denied", "unsupported", "disconnected"].contains(state)
            if self.terminalState {
              self.stop()
              self.state(state, status["reason"] as? String ?? "")
              return
            }
            if state == "streaming" { self.lastVideo = ProcessInfo.processInfo.systemUptime }
            self.state(state, status["reason"] as? String ?? "")
          case .data(let data):
            try self.decode(data)
            self.lastVideo = ProcessInfo.processInfo.systemUptime
          @unknown default: throw DesktopError.invalidFrame
          }
          // No unbounded dispatch backlog: request the next AU after this one.
          self.receive(socket, generation)
        } catch {
          self.stop()
          if !self.terminalState { self.state("disconnected", "Desktop connection ended.") }
        }
      }
    }
  }

  private func decode(_ data: Data) throws {
    let units = try annexB(data)
    let idr = units.contains { ($0.first ?? 0) & 31 == 5 }
    if needIDR && !idr { dropped += 1; return }
    if format == nil {
      guard let sps = units.first(where: { ($0.first ?? 0) & 31 == 7 }),
        let pps = units.first(where: { ($0.first ?? 0) & 31 == 8 }) else { return }
      let result = sps.withUnsafeBytes { s in
        pps.withUnsafeBytes { p in
          let pointers = [s.baseAddress!.assumingMemoryBound(to: UInt8.self), p.baseAddress!.assumingMemoryBound(to: UInt8.self)]
          let sizes = [sps.count, pps.count]
          return pointers.withUnsafeBufferPointer { ptr in
            sizes.withUnsafeBufferPointer { lengths in
              CMVideoFormatDescriptionCreateFromH264ParameterSets(allocator: kCFAllocatorDefault,
                parameterSetCount: 2, parameterSetPointers: ptr.baseAddress!, parameterSetSizes: lengths.baseAddress!,
                nalUnitHeaderLength: 4, formatDescriptionOut: &format)
            }
          }
        }
      }
      guard result == noErr else { throw DesktopError.invalidFrame }
    }
    if !video.isReadyForMoreMediaData { throw DesktopError.backpressure }
    var avcc = Data()
    for unit in units {
      var length = UInt32(unit.count).bigEndian
      withUnsafeBytes(of: &length) { avcc.append(contentsOf: $0) }
      avcc.append(unit)
    }
    var block: CMBlockBuffer?
    guard CMBlockBufferCreateWithMemoryBlock(allocator: kCFAllocatorDefault, memoryBlock: nil,
      blockLength: avcc.count, blockAllocator: kCFAllocatorDefault, customBlockSource: nil,
      offsetToData: 0, dataLength: avcc.count, flags: 0, blockBufferOut: &block) == noErr,
      let block else { throw DesktopError.invalidFrame }
    let copied = avcc.withUnsafeBytes { bytes in
      CMBlockBufferReplaceDataBytes(with: bytes.baseAddress!, blockBuffer: block, offsetIntoDestination: 0, dataLength: avcc.count)
    }
    guard copied == noErr else { throw DesktopError.invalidFrame }
    var timing = CMSampleTimingInfo(duration: .invalid, presentationTimeStamp: .zero, decodeTimeStamp: .invalid)
    var size = avcc.count
    var sample: CMSampleBuffer?
    guard CMSampleBufferCreateReady(allocator: kCFAllocatorDefault, dataBuffer: block,
      formatDescription: format, sampleCount: 1, sampleTimingEntryCount: 1, sampleTimingArray: &timing,
      sampleSizeEntryCount: 1, sampleSizeArray: &size, sampleBufferOut: &sample) == noErr,
      let sample else { throw DesktopError.invalidFrame }
    if let array = CMSampleBufferGetSampleAttachmentsArray(sample, createIfNecessary: true) {
      let attachments = unsafeBitCast(CFArrayGetValueAtIndex(array, 0), to: CFMutableDictionary.self)
      CFDictionarySetValue(attachments, Unmanaged.passUnretained(kCMSampleAttachmentKey_DisplayImmediately).toOpaque(), Unmanaged.passUnretained(kCFBooleanTrue).toOpaque())
    }
    video.enqueue(sample)
    guard video.status != .failed else { throw DesktopError.invalidFrame }
    submitted += 1; needIDR = false
  }

  func sendCommand(_ owner: String, _ sequence: Int, _ value: String) -> Bool {
    guard !owner.isEmpty, owner == inputGeneration, sequence == inputSequence + 1 else { return false }
    guard send(value) else { return false }
    inputSequence = sequence
    return true
  }

  func disconnect(_ owner: String) {
    if !owner.isEmpty && owner == inputGeneration { stop() }
  }

  @discardableResult
  private func send(_ value: String) -> Bool {
    guard !value.isEmpty, value.utf8.count <= 8192, let task else { return false }
    guard pendingSends < 4 else {
      stop(); state("disconnected", "Desktop input timed out."); return false
    }
    pendingSends += 1
    let generation = epoch
    task.send(.string(value)) { [weak self] error in
      DispatchQueue.main.async {
        guard let self, self.epoch == generation else { return }
        self.pendingSends -= 1
        if error != nil {
          self.stop(); self.state("disconnected")
        }
      }
    }
    return true
  }

  func stop() {
    epoch += 1
    heartbeat?.invalidate(); heartbeat = nil
    task?.cancel(with: .normalClosure, reason: nil); task = nil
    session?.invalidateAndCancel(); session = nil
    video.flushAndRemoveImage()
    format = nil; needIDR = true; submitted = 0; dropped = 0
    pendingSends = 0; lastVideo = nil
    connection = ""
    inputGeneration = ""; inputSequence = 0
    selectedSource = ""
  }
}

private enum DesktopError: Error { case invalidFrame, backpressure }

private func annexB(_ data: Data) throws -> [Data] {
  guard data.count <= 4 * 1024 * 1024 else { throw DesktopError.invalidFrame }
  let bytes = [UInt8](data)
  var starts: [(Int, Int)] = []
  var index = 0
  while index + 3 <= bytes.count {
    var prefix = 0
    if bytes[index] == 0 && bytes[index + 1] == 0 {
      if bytes[index + 2] == 1 { prefix = 3 }
      else if index + 4 <= bytes.count && bytes[index + 2] == 0 && bytes[index + 3] == 1 { prefix = 4 }
    }
    if prefix > 0 { starts.append((index, prefix)); index += prefix }
    else { index += 1 }
    if starts.count > 4096 { throw DesktopError.invalidFrame }
  }
  return starts.enumerated().compactMap { index, start in
    let end = index + 1 < starts.count ? starts[index + 1].0 : bytes.count
    return end > start.0 + start.1 ? Data(bytes[(start.0 + start.1)..<end]) : nil
  }
}
