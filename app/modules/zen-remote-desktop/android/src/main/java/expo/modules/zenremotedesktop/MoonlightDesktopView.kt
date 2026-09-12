package expo.modules.zenremotedesktop

import android.content.Context
import android.media.MediaCodec
import android.media.MediaFormat
import android.os.Handler
import android.os.HandlerThread
import android.view.Surface
import android.view.SurfaceHolder
import android.view.SurfaceView
import expo.modules.kotlin.AppContext
import expo.modules.kotlin.viewevent.EventDispatcher
import expo.modules.kotlin.views.ExpoView
import expo.modules.zenremotedesktop.moonlight.MoonlightHost
import expo.modules.zenremotedesktop.moonlight.MoonlightNvHttp
import expo.modules.zenremotedesktop.moonlight.MoonlightVideoFormats
import expo.modules.zenremotedesktop.moonlight.ZenCryptoProvider
import expo.modules.zenremotedesktop.moonlight.ZenHostTrustStore
import expo.modules.zenremotedesktop.moonlight.ZenMoonlightSession
import org.json.JSONObject
import java.io.File
import java.nio.ByteBuffer
import java.security.cert.X509Certificate
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Actual paired rendering path with per-connection ownership. Every connection
 * attempt owns a [Connection] record; core callbacks, decoder tasks, frame
 * listeners and input all check that their connection is still the current one,
 * so stale callbacks cannot mutate a newer attempt. Terminal states
 * (disconnected/revoked) are always observable, even though the attempt is
 * invalidated before the blocking host cleanup runs.
 *
 * Decoder recovery follows the pinned upstream client renderer: any condition
 * that prevents decoding (no surface, waiting for a keyframe, full queue,
 * buffer failure, decoder exception) returns DR_NEED_IDR (-1) so the core
 * requests a fresh keyframe instead of silently dropping frames.
 */
class MoonlightDesktopView(context: Context, appContext: AppContext) :
  ExpoView(context, appContext), SurfaceHolder.Callback {

  private val onState by EventDispatcher()
  private val surface = SurfaceView(context)
  private val cover = android.view.View(context).apply { setBackgroundColor(android.graphics.Color.BLACK) }
  private val thread = HandlerThread("ZenMoonlightDecoder").apply { start() }
  private val decoder = Handler(thread.looper)
  private val stateLock = Any()

  private var attemptSeq = 0L

  @Volatile
  private var currentAttempt = 0L

  @Volatile
  private var connection: Connection? = null

  @Volatile
  private var destroyed = false

  private var codec: MediaCodec? = null

  // Blocking connect runs on `background`; cancel/stop/revoke run on `control`
  // so a pairing that waits for a PIN can always be cancelled promptly.
  private val background = java.util.concurrent.Executors.newSingleThreadExecutor { runnable ->
    Thread(runnable, "zen-moonlight-session")
  }
  private val control = java.util.concurrent.Executors.newSingleThreadExecutor { runnable ->
    Thread(runnable, "zen-moonlight-control")
  }

  private inner class Connection(val attempt: Long) {
    val core = Core(this)
    var host: MoonlightHost? = null
    var session: ZenMoonlightSession? = null

    @Volatile
    var connected = false

    @Volatile
    var surfaceReady = false

    @Volatile
    var waitingIdr = true

    val queue = ArrayDeque<Pair<ByteArray, Int>>()
    val drainScheduled = AtomicBoolean(false)
    var width = 1280
    var height = 720
    var presented = 0
    var dropped = 0
    var framesSinceCodecStart = 0
  }

  private fun alive(conn: Connection): Boolean =
    !destroyed && connection === conn && currentAttempt == conn.attempt

  private fun attach(conn: Connection, host: MoonlightHost, session: ZenMoonlightSession): Boolean =
    synchronized(stateLock) {
      if (connection !== conn || currentAttempt != conn.attempt) {
        false
      } else {
        conn.host = host
        conn.session = session
        true
      }
    }

  private inner class Core(private val conn: Connection) : MoonlightCore() {
    override fun onVideoSetup(videoFormat: Int, videoWidth: Int, videoHeight: Int, redrawRate: Int): Int {
      if (!alive(conn)) return -1
      if (videoFormat != VIDEO_FORMAT_H264) {
        publish(conn, "failed", "unsupported_video_format:$videoFormat")
        return -1
      }
      conn.width = videoWidth
      conn.height = videoHeight
      conn.surfaceReady = surface.holder.surface?.isValid == true
      conn.waitingIdr = true
      decoder.post { if (alive(conn)) releaseCodecLocked() }
      return 0
    }

    override fun onDecodeUnit(data: ByteArray, frameType: Int, presentationTimeUs: Long): Int {
      if (!alive(conn) || !conn.surfaceReady) return -1
      if (conn.waitingIdr && frameType != MoonlightCore.FRAME_TYPE_IDR) {
        conn.dropped++
        return -1
      }
      synchronized(conn.queue) {
        if (conn.queue.size >= MAX_QUEUED_FRAMES) {
          conn.queue.clear()
          conn.dropped++
          conn.waitingIdr = true
          return -1
        }
        conn.queue.addLast(data to frameType)
      }
      scheduleDrain(conn)
      return 0
    }

    override fun onConnectionStarted() {
      if (!alive(conn)) return
      conn.connected = true
      publish(conn, "connected", "established")
    }

    override fun onConnectionStage(stage: Int) {
      if (alive(conn)) publish(conn, "stage", "stage:$stage")
    }

    override fun onConnectionStatusUpdate(connectionStatus: Int) {
      if (alive(conn)) publish(conn, "status", if (connectionStatus == 0) "okay" else "poor")
    }

    override fun onConnectionStartFailed(errorCode: Int) {
      if (alive(conn)) publish(conn, "failed", "start:$errorCode")
    }

    override fun onConnectionTerminated(errorCode: Int) {
      if (!alive(conn)) return
      conn.connected = false
      decoder.post { if (alive(conn)) releaseCodecLocked() }
      publish(conn, "disconnected", "host:$errorCode")
    }
  }

  init {
    setBackgroundColor(android.graphics.Color.BLACK)
    addView(surface, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
    addView(cover, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
    surface.holder.addCallback(this)
  }

  fun connect(value: String) {
    if (destroyed) return
    if (value.isEmpty()) {
      disconnectCurrent()
      return
    }
    val config = try {
      JSONObject(value)
    } catch (_: Exception) {
      publishDetached("rejected", "invalid_config")
      return
    }
    val hostName = config.optString("host").trim()
    val identityKey = config.optString("identityKey").trim()
    val hostKey = config.optString("hostKey").trim()
    if (hostName.isEmpty()) {
      publishDetached("rejected", "missing_host")
      return
    }
    // Stable Zen device identity plus Zen host key from the authenticated
    // bootstrap; never a lossy hostname/port substitution.
    if (!identityKey.matches(Regex("[A-Za-z0-9._-]{1,128}")) || !hostKey.matches(Regex("[A-Za-z0-9._-]{1,128}"))) {
      publishDetached("rejected", "missing_identity")
      return
    }
    val ports = intArrayOf(config.optInt("httpPort", 47989), config.optInt("httpsPort", 47984))
    if (ports[0] !in 1..65535 || ports[1] !in 1..65535) {
      publishDetached("rejected", "invalid_ports")
      return
    }

    val (conn, previous) = synchronized(stateLock) {
      val old = connection
      val next = Connection(++attemptSeq)
      currentAttempt = next.attempt
      connection = next
      next to old
    }
    previous?.let { stopConnectionDetached(it) }

    val plan = ZenMoonlightSession.Plan(
      appId = config.optInt("appId", 1),
      width = config.optInt("width", 1280),
      height = config.optInt("height", 720),
      fps = config.optInt("fps", 30),
      bitrateKbps = config.optInt("bitrateKbps", 8_000),
      packetSize = config.optInt("packetSize", 1024),
      audioConfiguration = config.optInt("audioConfiguration", MoonlightAudioConfiguration.STEREO),
      supportedVideoFormats = config.optInt("supportedVideoFormats", MoonlightVideoFormats.DECODER_SUPPORTED),
      streamingRemotely = config.optInt("streamingRemotely", -1),
      enableSops = config.optBoolean("enableSops", true),
    )
    val pin = config.optString("pin").takeIf { it.isNotBlank() }
    publish(conn, "connecting", hostName)

    background.execute {
      try {
        val identityDir = identityDirectory(identityKey)
        val hostDir = hostDirectory(identityKey, hostKey)
        val store = ZenHostTrustStore(hostDir)
        val crypto = ZenCryptoProvider(identityDir)
        val pinned: X509Certificate? = try {
          store.load()
        } catch (_: Exception) {
          null
        }
        val hostClient = MoonlightNvHttp(hostName, ports[0], ports[1], identityKey, "zen", pinned, crypto)
        val sessionControl = object : ZenMoonlightSession.CoreControl {
          override fun start(config: MoonlightCore.Config): Int = if (alive(conn)) conn.core.start(config) else -2

          override fun stop(): Int = conn.core.stop()
        }
        val activeSession = ZenMoonlightSession(store, sessionControl)
        if (!attach(conn, hostClient, activeSession)) {
          activeSession.stop(hostClient)
          return@execute
        }
        when (val result = activeSession.connect(hostClient, plan, pin)) {
          is ZenMoonlightSession.Result.Rejected -> publish(conn, "rejected", result.reason)
          is ZenMoonlightSession.Result.Started ->
            publish(conn, if (result.startAccepted) "start_accepted" else "start_failed", result.launchVerb)
        }
      } catch (error: Exception) {
        publish(conn, "rejected", "connect_failed:${error.javaClass.simpleName}")
      }
    }
  }

  fun disconnect(generationValue: Int): Boolean {
    val conn = currentConnection(generationValue) ?: return false
    invalidate(conn)
    stopConnectionDetached(conn)
    publishTerminal(conn, "disconnected", "user")
    return true
  }

  fun revoke(generationValue: Int): Boolean {
    val conn = currentConnection(generationValue) ?: return false
    invalidate(conn)
    // Blocking host cleanup runs off the UI thread on the control executor, so
    // it is never queued behind a blocked connect.
    control.execute {
      val hostClient = conn.host
      val activeSession = conn.session
      val detail = if (activeSession != null && hostClient != null) {
        val report = activeSession.revoke(hostClient)
        if (report.complete) "complete" else report.failures.joinToString(",")
      } else {
        conn.core.stop()
        "no_session"
      }
      publishTerminal(conn, "revoked", detail)
    }
    return true
  }

  fun sendKey(generationValue: Int, keyCode: Int, keyAction: Int, modifiers: Int, flags: Int): Boolean {
    val conn = currentConnection(generationValue) ?: return false
    if (!conn.connected) return false
    return conn.core.sendKeyboardEvent(keyCode.toShort(), keyAction.toByte(), modifiers.toByte(), flags.toByte()) == 0
  }

  fun sendText(generationValue: Int, value: String): Boolean {
    val conn = currentConnection(generationValue) ?: return false
    if (!conn.connected || value.isEmpty()) return false
    return conn.core.sendUtf8TextEvent(value) == 0
  }

  fun sendPointerMove(generationValue: Int, deltaX: Int, deltaY: Int): Boolean {
    val conn = currentConnection(generationValue) ?: return false
    if (!conn.connected) return false
    return conn.core.sendMouseMove(deltaX.toShort(), deltaY.toShort()) == 0
  }

  /** Absolute pointer position in the streamed frame's coordinate space. */
  fun sendPointerPosition(generationValue: Int, x: Int, y: Int, referenceWidth: Int, referenceHeight: Int): Boolean {
    val conn = currentConnection(generationValue) ?: return false
    if (!conn.connected || referenceWidth <= 0 || referenceHeight <= 0) return false
    return conn.core.sendMousePosition(
      x.coerceIn(0, 32767).toShort(),
      y.coerceIn(0, 32767).toShort(),
      referenceWidth.coerceIn(1, 32767).toShort(),
      referenceHeight.coerceIn(1, 32767).toShort(),
    ) == 0
  }

  fun sendPointerButton(generationValue: Int, button: Int, action: Int): Boolean {
    val conn = currentConnection(generationValue) ?: return false
    if (!conn.connected) return false
    return conn.core.sendMouseButton(action.toByte(), button) == 0
  }

  fun sendScroll(generationValue: Int, clicks: Int): Boolean {
    val conn = currentConnection(generationValue) ?: return false
    if (!conn.connected) return false
    return conn.core.sendScroll(clicks.toByte()) == 0
  }

  private fun currentConnection(generationValue: Int): Connection? {
    if (generationValue <= 0) return null
    val conn = connection ?: return null
    return if (conn.attempt == generationValue.toLong() && alive(conn)) conn else null
  }

  private fun invalidate(conn: Connection) = synchronized(stateLock) {
    if (connection === conn) {
      currentAttempt = ++attemptSeq
      connection = null
    }
    // Promptly cancel any blocked HTTP/pairing call on the connect thread.
    conn.host?.cancelInFlight()
  }

  private fun disconnectCurrent() {
    val conn = connection ?: return
    invalidate(conn)
    stopConnectionDetached(conn)
    publishTerminal(conn, "disconnected", "cleared")
  }

  private fun stopConnectionDetached(conn: Connection) {
    val hostClient = conn.host
    val activeSession = conn.session
    control.execute {
      if (activeSession != null) {
        activeSession.stop(hostClient)
      } else {
        conn.core.stop()
      }
    }
  }

  private fun identityDirectory(identityKey: String): File {
    val dir = File(context.filesDir, "zen-desktop/identity/$identityKey")
    if (!dir.exists() && !dir.mkdirs()) {
      throw IllegalStateException("identity_dir_unavailable")
    }
    return dir
  }

  private fun hostDirectory(identityKey: String, hostKey: String): File {
    val dir = File(context.filesDir, "zen-desktop/hosts/$identityKey/$hostKey")
    if (!dir.exists() && !dir.mkdirs()) {
      throw IllegalStateException("host_dir_unavailable")
    }
    return dir
  }

  private fun scheduleDrain(conn: Connection) {
    if (!conn.drainScheduled.compareAndSet(false, true)) return
    if (!decoder.post {
        try {
          drainQueue(conn)
        } finally {
          conn.drainScheduled.set(false)
          // Re-check after clearing the flag: frames queued during the drain
          // would otherwise be lost until the next submit.
          if (alive(conn) && synchronized(conn.queue) { conn.queue.isNotEmpty() }) {
            scheduleDrain(conn)
          }
        }
      }) {
      conn.drainScheduled.set(false)
    }
  }

  private fun drainQueue(conn: Connection) {
    if (!alive(conn)) {
      synchronized(conn.queue) { conn.queue.clear() }
      return
    }
    repeat(MAX_DRAIN_PER_TICK) {
      val next = synchronized(conn.queue) { if (conn.queue.isEmpty()) null else conn.queue.removeFirst() } ?: return
      if (!alive(conn)) return
      decode(conn, next.first, next.second)
    }
  }

  private fun decode(conn: Connection, data: ByteArray, frameType: Int) {
    val units = try {
      AnnexB.units(data)
    } catch (_: Exception) {
      conn.dropped++
      return
    }
    val idr = units.any { (it[0].toInt() and 31) == 5 }
    if (conn.waitingIdr && !idr) {
      conn.dropped++
      return
    }
    var active = codec
    if (active == null) {
      val sps = units.firstOrNull { (it[0].toInt() and 31) == 7 } ?: return
      val pps = units.firstOrNull { (it[0].toInt() and 31) == 8 } ?: return
      val target: Surface = surface.holder.surface ?: return
      if (!target.isValid) return
      active = try {
        createCodec(conn, sps, pps, target)
      } catch (_: Exception) {
        recoverDecoder(conn, "decoder_config_failed")
        return
      } ?: return
    }

    val index = try {
      active.dequeueInputBuffer(10_000)
    } catch (_: Exception) {
      recoverDecoder(conn, "decoder_dequeue_failed")
      return
    }
    if (index < 0) {
      recoverDecoder(conn, "decoder_backpressure")
      return
    }
    val input = try {
      active.getInputBuffer(index)
    } catch (_: Exception) {
      null
    }
    if (input == null || input.capacity() < data.size) {
      // Never abandon a held input buffer: return it to the codec first.
      try {
        active.queueInputBuffer(index, 0, 0, 0, 0)
      } catch (_: Exception) {
      }
      recoverDecoder(conn, "decoder_buffer_too_small")
      return
    }
    try {
      input.clear()
      input.put(data)
      val flags = if (frameType == MoonlightCore.FRAME_TYPE_IDR) MediaCodec.BUFFER_FLAG_KEY_FRAME else 0
      active.queueInputBuffer(index, 0, data.size, System.nanoTime() / 1000, flags)
      conn.waitingIdr = false
      drainOutput(conn, active)
    } catch (_: Exception) {
      recoverDecoder(conn, "decoder_queue_failed")
    }
  }

  private fun createCodec(conn: Connection, sps: ByteArray, pps: ByteArray, target: Surface): MediaCodec {
    val format = MediaFormat.createVideoFormat("video/avc", conn.width, conn.height)
    format.setByteBuffer("csd-0", ByteBuffer.wrap(byteArrayOf(0, 0, 0, 1) + sps))
    format.setByteBuffer("csd-1", ByteBuffer.wrap(byteArrayOf(0, 0, 0, 1) + pps))
    format.setInteger(MediaFormat.KEY_MAX_INPUT_SIZE, MAX_DECODE_UNIT_BYTES)
    if (android.os.Build.VERSION.SDK_INT >= 30) {
      format.setInteger(MediaFormat.KEY_LOW_LATENCY, 1)
    }
    val created = MediaCodec.createDecoderByType("video/avc")
    try {
      created.configure(format, target, null, 0)
    } catch (error: Exception) {
      try {
        created.release()
      } catch (_: Exception) {
      }
      throw error
    }
    created.setOnFrameRenderedListener({ _, _, _ ->
      if (!alive(conn)) return@setOnFrameRenderedListener
      conn.presented++
      if (conn.presented == 1) {
        post { if (alive(conn)) cover.visibility = GONE }
        publish(conn, "frame", "first")
      }
    }, decoder)
    try {
      created.start()
    } catch (error: Exception) {
      try {
        created.release()
      } catch (_: Exception) {
      }
      throw error
    }
    codec = created
    conn.framesSinceCodecStart = 0
    conn.waitingIdr = true
    conn.surfaceReady = target.isValid
    return created
  }

  private fun drainOutput(conn: Connection, active: MediaCodec) {
    val info = MediaCodec.BufferInfo()
    repeat(16) {
      val index = try {
        active.dequeueOutputBuffer(info, 0)
      } catch (_: Exception) {
        recoverDecoder(conn, "decoder_output_failed")
        return
      }
      if (index >= 0) {
        active.releaseOutputBuffer(index, true)
      } else if (index == MediaCodec.INFO_TRY_AGAIN_LATER) {
        return
      }
    }
  }

  private fun recoverDecoder(conn: Connection, reason: String) {
    releaseCodecLocked()
    conn.waitingIdr = true
    if (alive(conn)) publish(conn, "failed", reason)
  }

  private fun releaseCodecLocked() {
    try {
      codec?.stop()
    } catch (_: Exception) {
    }
    try {
      codec?.release()
    } catch (_: Exception) {
    }
    codec = null
    connection?.let { conn ->
      conn.waitingIdr = true
    }
    post { cover.visibility = VISIBLE }
  }

  /** Emits a rejection before any connection record exists. */
  private fun publishDetached(state: String, reason: String) {
    val attempt = currentAttempt.toInt()
    val event = mapOf(
      "state" to state,
      "reason" to reason,
      "generation" to attempt,
      "width" to 0,
      "height" to 0,
      "presented" to 0,
      "dropped" to 0,
      "connected" to false,
    )
    if (android.os.Looper.myLooper() == android.os.Looper.getMainLooper()) {
      onState(event)
    } else {
      post { if (!destroyed) onState(event) }
    }
  }

  private fun publish(conn: Connection, state: String, reason: String) {
    if (!alive(conn)) return
    val event = eventBody(conn, state, reason)
    if (android.os.Looper.myLooper() == android.os.Looper.getMainLooper()) {
      onState(event)
    } else {
      post { if (alive(conn)) onState(event) }
    }
  }

  /** Terminal events are observable even though the attempt was invalidated. */
  private fun publishTerminal(conn: Connection, state: String, reason: String) {
    val event = eventBody(conn, state, reason)
    if (android.os.Looper.myLooper() == android.os.Looper.getMainLooper()) {
      onState(event)
    } else {
      post { if (!destroyed) onState(event) }
    }
  }

  private fun eventBody(conn: Connection, state: String, reason: String): Map<String, Any> = mapOf(
    "state" to state,
    "reason" to reason,
    "generation" to conn.attempt.toInt(),
    "width" to conn.width,
    "height" to conn.height,
    "presented" to conn.presented,
    "dropped" to conn.dropped,
    "connected" to conn.connected,
  )

  fun destroy() {
    if (destroyed) return
    destroyed = true
    val conn = synchronized(stateLock) {
      val old = connection
      connection = null
      currentAttempt = ++attemptSeq
      old
    }
    // Cleanup is queued on the control executor so an already requested revoke
    // (which keeps the pinned certificate until its authenticated quit) runs
    // first and is not cleared by destruction.
    control.execute {
      conn?.session?.stop(conn.host) ?: conn?.core?.stop()
      control.shutdown()
      background.shutdown()
    }
    decoder.post {
      releaseCodecLocked()
      thread.quitSafely()
    }
  }

  override fun surfaceCreated(holder: SurfaceHolder) {
    val conn = connection ?: return
    conn.surfaceReady = holder.surface?.isValid == true
    conn.waitingIdr = true
  }

  override fun surfaceChanged(holder: SurfaceHolder, format: Int, surfaceWidth: Int, surfaceHeight: Int) {
    val conn = connection ?: return
    conn.surfaceReady = holder.surface?.isValid == true
    conn.waitingIdr = true
  }

  override fun surfaceDestroyed(holder: SurfaceHolder) {
    // Keep the session; release the decoder and let the core send a fresh IDR
    // once a surface returns (onDecodeUnit returns DR_NEED_IDR meanwhile).
    val conn = connection
    if (conn != null) {
      conn.surfaceReady = false
      conn.waitingIdr = true
    }
    decoder.post { releaseCodecLocked() }
  }

  private companion object {
    const val MAX_QUEUED_FRAMES = 4
    const val MAX_DRAIN_PER_TICK = 4
    const val MAX_DECODE_UNIT_BYTES = 4 * 1024 * 1024
  }
}
