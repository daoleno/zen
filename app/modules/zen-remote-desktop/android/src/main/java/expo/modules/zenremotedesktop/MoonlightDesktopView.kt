package expo.modules.zenremotedesktop

import android.content.Context
import android.media.MediaCodec
import android.media.MediaFormat
import android.os.Handler
import android.os.HandlerThread
import android.util.Log
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
import java.util.concurrent.atomic.AtomicInteger

/**
 * Actual paired rendering path: per-host Zen identity -> upstream
 * pairing/launch through MoonlightNvHttp -> MoonlightCore -> MediaCodec on a
 * real SurfaceView, with keyboard/pointer/scroll input and disconnect/revoke on
 * the same connection.
 *
 * startAccepted, connectionStarted and the first rendered frame are distinct
 * observations reported to JS as separate states.
 */
class MoonlightDesktopView(context: Context, appContext: AppContext) :
  ExpoView(context, appContext), SurfaceHolder.Callback {

  private val onState by EventDispatcher()
  private val surface = SurfaceView(context)
  private val cover = android.view.View(context).apply { setBackgroundColor(android.graphics.Color.BLACK) }
  private val thread = HandlerThread("ZenMoonlightDecoder").apply { start() }
  private val decoder = Handler(thread.looper)
  private val generation = AtomicInteger(0)

  @Volatile
  private var destroyed = false

  @Volatile
  private var codec: MediaCodec? = null
  private var width = 1280
  private var height = 720
  private var needIDR = true
  private var presented = 0
  private var dropped = 0
  private val queue = ArrayDeque<Pair<ByteArray, Int>>()

  @Volatile
  private var connected = false

  @Volatile
  private var renderedFirstFrame = false

  private var core: MoonlightCore? = null
  private var session: ZenMoonlightSession? = null
  private var host: MoonlightHost? = null
  private var trustStore: ZenHostTrustStore? = null
  private var connectThread: Thread? = null

  private inner class Core : MoonlightCore() {
    override fun onVideoSetup(videoFormat: Int, videoWidth: Int, videoHeight: Int, redrawRate: Int): Int {
      // The wired renderer is H.264 only until another decoder path exists.
      if (videoFormat != MoonlightCore.VIDEO_FORMAT_H264) {
        publish("failed", "unsupported_video_format:$videoFormat")
        return -1
      }
      width = videoWidth
      height = videoHeight
      decoder.post { ensureCodec() }
      return 0
    }

    override fun onDecodeUnit(data: ByteArray, frameType: Int, presentationTimeUs: Long): Int {
      if (needIDR && frameType != MoonlightCore.FRAME_TYPE_IDR) {
        dropped++
        return 0
      }
      synchronized(queue) {
        if (queue.size >= MAX_QUEUED_FRAMES) {
          val droppedFrame = queue.removeFirst()
          dropped++
          if (droppedFrame.second == MoonlightCore.FRAME_TYPE_IDR) {
            needIDR = true
          }
        }
        queue.addLast(data to frameType)
      }
      decoder.post { drainQueue() }
      return 0
    }

    override fun onConnectionStarted() {
      connected = true
      publish("connected", "established")
    }

    override fun onConnectionStage(stage: Int) {
      publish("stage", "stage:$stage")
    }

    override fun onConnectionStatusUpdate(connectionStatus: Int) {
      publish("status", if (connectionStatus == 0) "okay" else "poor")
    }

    override fun onConnectionStartFailed(errorCode: Int) {
      publish("failed", "start:$errorCode")
    }

    override fun onConnectionTerminated(errorCode: Int) {
      connected = false
      releaseCodec()
      publish("disconnected", "host:$errorCode")
    }
  }

  init {
    setBackgroundColor(android.graphics.Color.BLACK)
    addView(surface, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
    addView(cover, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
    surface.holder.addCallback(this)
  }

  fun connect(value: String) {
    if (destroyed || value.isEmpty()) return
    val config = try {
      JSONObject(value)
    } catch (_: Exception) {
      publish("rejected", "invalid_config")
      return
    }
    val epoch = generation.incrementAndGet()
    stopInternal(epoch, publishState = false)

    val hostName = config.optString("host")
    if (hostName.isBlank()) {
      publish("rejected", "missing_host", epoch)
      return
    }
    val httpPort = config.optInt("httpPort", 47989)
    val httpsPort = config.optInt("httpsPort", 47984)
    val uniqueId = config.optString("uniqueId")
    if (uniqueId.isBlank()) {
      publish("rejected", "missing_identity", epoch)
      return
    }
    val appId = config.optInt("appId", 1)
    val pin = config.optString("pin").takeIf { it.isNotBlank() }
    val plan = ZenMoonlightSession.Plan(
      appId = appId,
      width = config.optInt("width", 1920),
      height = config.optInt("height", 1080),
      fps = config.optInt("fps", 60),
      bitrateKbps = config.optInt("bitrateKbps", 20_000),
      packetSize = config.optInt("packetSize", 1024),
      audioConfiguration = config.optInt("audioConfiguration", MoonlightAudioConfiguration.STEREO),
      // Decoder-supported formats; H.264 only until another decoder exists.
      supportedVideoFormats = config.optInt("supportedVideoFormats", MoonlightVideoFormats.DECODER_SUPPORTED),
      streamingRemotely = config.optInt("streamingRemotely", -1),
      enableSops = config.optBoolean("enableSops", true),
    )

    val active = Core()
    core = active
    publish("connecting", hostName, epoch)

    val worker = Thread({
      try {
        val identityDir = identityDirectory(hostName)
        val store = ZenHostTrustStore(identityDir)
        trustStore = store
        val crypto = ZenCryptoProvider(identityDir)
        val hostClient = MoonlightNvHttp(hostName, httpPort, httpsPort, uniqueId, "zen",
          loadPinned(store), crypto)
        host = hostClient
        val sessionControl = object : ZenMoonlightSession.CoreControl {
          override fun start(config: MoonlightCore.Config): Int = active.start(config)
          override fun stop(): Int = active.stop()
        }
        val activeSession = ZenMoonlightSession(store, sessionControl)
        session = activeSession
        if (epoch != generation.get()) return@Thread
        when (val result = activeSession.connect(hostClient, plan, pin)) {
          is ZenMoonlightSession.Result.Rejected -> publish("rejected", result.reason, epoch)
          is ZenMoonlightSession.Result.Started ->
            publish(
              if (result.startAccepted) "start_accepted" else "start_failed",
              "${result.launchVerb}:${result.rtspSessionUrl ?: ""}",
              epoch,
            )
        }
      } catch (error: Exception) {
        publish("rejected", "connect_failed:${error.javaClass.simpleName}", epoch)
      }
    }, "zen-moonlight-connect")
    connectThread = worker
    worker.start()
  }

  fun disconnect(generationValue: Int): Boolean {
    if (generationValue == 0 || generationValue != generation.get()) return false
    stopInternal(generationValue, publishState = true)
    return true
  }

  fun revoke(generationValue: Int): Boolean {
    if (generationValue == 0 || generationValue != generation.get()) return false
    val activeSession = session
    val activeHost = host
    generation.incrementAndGet()
    releaseCodec()
    connected = false
    if (activeSession != null && activeHost != null) {
      val report = activeSession.revoke(activeHost)
      publish("revoked", if (report.complete) "complete" else report.failures.joinToString(","), generationValue)
    } else {
      publish("revoked", "no_session", generationValue)
    }
    session = null
    host = null
    return true
  }

  fun sendKey(generationValue: Int, keyCode: Int, keyAction: Int, modifiers: Int, flags: Int): Boolean {
    if (generationValue != generation.get() || !connected) return false
    return core?.sendKeyboardEvent(keyCode.toShort(), keyAction.toByte(), modifiers.toByte(), flags.toByte()) == 0
  }

  fun sendText(generationValue: Int, value: String): Boolean {
    if (generationValue != generation.get() || !connected || value.isEmpty()) return false
    return core?.sendUtf8TextEvent(value) == 0
  }

  fun sendPointerMove(generationValue: Int, deltaX: Int, deltaY: Int): Boolean {
    if (generationValue != generation.get() || !connected) return false
    return core?.sendMouseMove(deltaX.toShort(), deltaY.toShort()) == 0
  }

  fun sendPointerButton(generationValue: Int, button: Int, action: Int): Boolean {
    if (generationValue != generation.get() || !connected) return false
    return core?.sendMouseButton(action.toByte(), button) == 0
  }

  fun sendScroll(generationValue: Int, clicks: Int): Boolean {
    if (generationValue != generation.get() || !connected) return false
    return core?.sendScroll(clicks.toByte()) == 0
  }

  private fun loadPinned(store: ZenHostTrustStore): X509Certificate? =
    try {
      store.load()
    } catch (_: Exception) {
      null
    }

  private fun identityDirectory(hostName: String): File {
    val safe = hostName.replace(Regex("[^A-Za-z0-9._-]"), "_")
    val dir = File(context.filesDir, "zen-desktop/$safe")
    if (!dir.exists() && !dir.mkdirs()) {
      throw IllegalStateException("identity_dir_unavailable")
    }
    return dir
  }

  private fun stopInternal(epoch: Int, publishState: Boolean) {
    releaseCodec()
    connected = false
    val activeSession = session
    val activeHost = host
    if (activeSession != null) {
      activeSession.stop(activeHost)
    }
    session = null
    host = null
    if (publishState) publish("disconnected", "user", epoch)
  }

  private fun drainQueue() {
    val epoch = generation.get()
    repeat(MAX_DRAIN_PER_TICK) {
      val next = synchronized(queue) { if (queue.isEmpty()) null else queue.removeFirst() } ?: return
      if (epoch != generation.get()) return
      decode(next.first, next.second)
    }
  }

  private fun ensureCodec() {
    if (codec != null || destroyed) return
    val surfaceReady = surface.holder.surface
    if (surfaceReady == null || !surfaceReady.isValid) return
    // Codec creation happens in decode() once SPS/PPS are available.
  }

  private fun decode(data: ByteArray, frameType: Int) {
    if (destroyed) return
    val units = try {
      AnnexB.units(data)
    } catch (_: Exception) {
      dropped++
      return
    }
    val idr = units.any { (it[0].toInt() and 31) == 5 }
    if (needIDR && !idr) {
      dropped++
      return
    }
    if (codec == null) {
      val sps = units.firstOrNull { (it[0].toInt() and 31) == 7 } ?: return
      val pps = units.firstOrNull { (it[0].toInt() and 31) == 8 } ?: return
      val target: Surface = surface.holder.surface ?: return
      if (!target.isValid) return
      val format = MediaFormat.createVideoFormat("video/avc", width, height)
      format.setByteBuffer("csd-0", ByteBuffer.wrap(byteArrayOf(0, 0, 0, 1) + sps))
      format.setByteBuffer("csd-1", ByteBuffer.wrap(byteArrayOf(0, 0, 0, 1) + pps))
      format.setInteger(MediaFormat.KEY_MAX_INPUT_SIZE, 4 * 1024 * 1024)
      if (android.os.Build.VERSION.SDK_INT >= 30) {
        format.setInteger(MediaFormat.KEY_LOW_LATENCY, 1)
      }
      val created = try {
        MediaCodec.createDecoderByType("video/avc")
      } catch (error: Exception) {
        publish("failed", "decoder_unavailable")
        return
      }
      created.configure(format, target, null, 0)
      created.setOnFrameRenderedListener({ _, _, _ ->
        presented++
        if (!renderedFirstFrame) {
          renderedFirstFrame = true
          post { cover.visibility = GONE }
          publish("frame", "first")
        }
      }, decoder)
      created.start()
      codec = created
      needIDR = true
    }
    val active = codec ?: return
    val index = try {
      active.dequeueInputBuffer(10_000)
    } catch (_: Exception) {
      releaseCodec()
      return
    }
    if (index < 0) {
      dropped++
      needIDR = true
      return
    }
    val input = active.getInputBuffer(index)
    if (input == null || input.capacity() < data.size) {
      dropped++
      needIDR = true
      return
    }
    input.clear()
    input.put(data)
    val flags = if (frameType == MoonlightCore.FRAME_TYPE_IDR) MediaCodec.BUFFER_FLAG_KEY_FRAME else 0
    active.queueInputBuffer(index, 0, data.size, System.nanoTime() / 1000, flags)
    needIDR = false
    drainOutput(active)
  }

  private fun drainOutput(active: MediaCodec) {
    val info = MediaCodec.BufferInfo()
    repeat(16) {
      val index = try {
        active.dequeueOutputBuffer(info, 0)
      } catch (_: Exception) {
        releaseCodec()
        return
      }
      if (index >= 0) {
        active.releaseOutputBuffer(index, true)
      } else if (index == MediaCodec.INFO_TRY_AGAIN_LATER) {
        return
      }
    }
  }

  private fun releaseCodec() {
    decoder.post {
      try {
        codec?.stop()
      } catch (_: Exception) {
      }
      codec?.release()
      codec = null
      needIDR = true
      renderedFirstFrame = false
      synchronized(queue) { queue.clear() }
      post { cover.visibility = VISIBLE }
    }
  }

  private fun publish(state: String, reason: String, epoch: Int = generation.get()) {
    if (destroyed || epoch != generation.get()) return
    val event = mapOf(
      "state" to state,
      "reason" to reason,
      "generation" to epoch,
      "width" to width,
      "height" to height,
      "presented" to presented,
      "dropped" to dropped,
      "connected" to connected,
    )
    if (android.os.Looper.myLooper() == android.os.Looper.getMainLooper()) {
      onState(event)
    } else {
      post { if (!destroyed && epoch == generation.get()) onState(event) }
    }
  }

  fun destroy() {
    if (destroyed) return
    destroyed = true
    generation.incrementAndGet()
    val activeSession = session
    val activeHost = host
    if (activeSession != null) {
      activeSession.stop(activeHost)
    }
    session = null
    host = null
    releaseCodec()
    decoder.post { thread.quitSafely() }
  }

  override fun surfaceCreated(holder: SurfaceHolder) {
    if (!destroyed && codec == null) needIDR = true
  }

  override fun surfaceChanged(holder: SurfaceHolder, format: Int, surfaceWidth: Int, surfaceHeight: Int) {}

  override fun surfaceDestroyed(holder: SurfaceHolder) {
    // Keep the session; release the decoder so stream frames drop until the
    // surface returns and an IDR reconfigures it.
    releaseCodec()
  }

  private companion object {
    const val MAX_QUEUED_FRAMES = 4
    const val MAX_DRAIN_PER_TICK = 4
  }
}
