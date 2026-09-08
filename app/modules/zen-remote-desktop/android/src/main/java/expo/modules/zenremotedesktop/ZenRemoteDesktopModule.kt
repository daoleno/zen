package expo.modules.zenremotedesktop

import android.content.Context
import android.media.MediaCodec
import android.media.MediaFormat
import android.os.Handler
import android.os.HandlerThread
import android.view.SurfaceHolder
import android.view.SurfaceView
import expo.modules.kotlin.AppContext
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import expo.modules.kotlin.viewevent.EventDispatcher
import expo.modules.kotlin.views.ExpoView
import okhttp3.*
import okio.ByteString
import org.json.JSONObject
import java.nio.ByteBuffer
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

class ZenRemoteDesktopModule : Module() {
  override fun definition() = ModuleDefinition {
    Name("ZenRemoteDesktop")
    View(DesktopView::class) {
      Events("onState")
      Prop("connection") { view: DesktopView, value: String -> view.connect(value) }
      AsyncFunction("sendCommand") { view: DesktopView, generation: String, sequence: Int, value: String ->
        view.sendCommand(generation, sequence, value)
      }
      AsyncFunction("disconnect") { view: DesktopView, generation: String -> view.disconnect(generation) }
      OnViewDestroys { view: DesktopView -> view.destroy() }
    }
  }
}

class DesktopView(context: Context, appContext: AppContext) : ExpoView(context, appContext), SurfaceHolder.Callback {
  private val onState by EventDispatcher()
  private val surface = SurfaceView(context)
  private val cover = android.view.View(context).apply { setBackgroundColor(android.graphics.Color.BLACK) }
  private val thread = HandlerThread("ZenDesktopDecoder").apply { start() }
  private val decoder = Handler(thread.looper)
  private val queued = AtomicInteger(0)
  private val generation = AtomicInteger(0)
  private val client = OkHttpClient.Builder().readTimeout(0, TimeUnit.MILLISECONDS)
    .connectTimeout(10, TimeUnit.SECONDS).writeTimeout(2, TimeUnit.SECONDS).build()
  private var socket: WebSocket? = null
  private var pendingConnection = ""
  private var inputGeneration = ""
  private var inputSequence = 0
  private var codec: MediaCodec? = null
  private var width = 1280
  private var height = 720
  private var needIDR = true
  private var presented = 0
  private var dropped = 0
  private var destroyed = false
  @Volatile private var lastFrame = 0L
  private var terminalState = false
  private val heartbeat = object : Runnable {
    override fun run() {
      send("{\"type\":\"ping\"}")
      if (lastFrame > 0 && android.os.SystemClock.elapsedRealtime() - lastFrame > 10000) {
        stop(); state("disconnected", "Desktop video timed out."); return
      }
      if (socket != null) postDelayed(this, 3000)
    }
  }

  init {
    setBackgroundColor(android.graphics.Color.BLACK)
    addView(surface, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
    addView(cover, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
    surface.holder.addCallback(this)
  }

  override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
    val availableW = r - l
    val availableH = b - t
    val scale = minOf(availableW.toFloat() / width, availableH.toFloat() / height)
    val w = (width * scale).toInt()
    val h = (height * scale).toInt()
    surface.layout((availableW - w) / 2, (availableH - h) / 2, (availableW + w) / 2, (availableH + h) / 2)
    cover.layout(0, 0, availableW, availableH)
  }

  private fun state(value: String, reason: String = "", epoch: Int = generation.get()) {
    val event = mapOf("state" to value, "reason" to reason, "width" to width, "height" to height, "presented" to presented, "dropped" to dropped)
    if (android.os.Looper.myLooper() == android.os.Looper.getMainLooper()) {
      if (epoch == generation.get() && !destroyed) onState(event)
    } else post { if (epoch == generation.get() && !destroyed) onState(event) }
  }

  fun connect(value: String) {
    if (destroyed || value == pendingConnection) return
    stop()
    terminalState = false
    if (value.isEmpty()) return
    try {
      inputGeneration = JSONObject(value).getString("inputGeneration")
      require(inputGeneration.isNotEmpty())
    } catch (_: Exception) { state("disconnected", "Invalid desktop connection."); return }
    pendingConnection = value
    if (!surface.holder.surface.isValid) return
    open(value)
  }

  private fun open(value: String) {
    val epoch = generation.get()
    try {
      val config = JSONObject(value)
      require(config.getString("inputGeneration") == inputGeneration && inputGeneration.isNotEmpty())
      val url = config.getString("url")
      val uri = android.net.Uri.parse(url)
      require(uri.scheme == "wss" || (uri.scheme == "ws" && uri.host == "127.0.0.1"))
      require(uri.path == "/desktop" && uri.query == null && uri.encodedUserInfo == null)
      val request = Request.Builder().url(url).header("Authorization", config.getString("authorization")).build()
      socket = client.newWebSocket(request, object : WebSocketListener() {
        override fun onOpen(ws: WebSocket, response: Response) {
          if (epoch != generation.get()) { ws.cancel(); return }
          post { if (epoch == generation.get()) { removeCallbacks(heartbeat); post(heartbeat) } }
        }
        override fun onMessage(ws: WebSocket, text: String) {
          if (epoch != generation.get()) return
          if (text.toByteArray(Charsets.UTF_8).size > 8192) { ws.cancel(); return }
          try {
            val status = JSONObject(text)
            // Validate on the receiving thread before posting asynchronous work.
            val value = status.getString("state")
            require(value in listOf("sources", "requesting", "streaming", "denied", "unsupported", "disconnected"))
            require(status.has("width") == status.has("height"))
            val nextWidth = if (status.has("width")) status.getInt("width") else null
            val nextHeight = if (status.has("height")) status.getInt("height") else null
            if (nextWidth != null && nextHeight != null) {
              require(nextWidth in 2..4096 && nextHeight in 2..4096)
            }
            if (queued.incrementAndGet() > 2) {
              queued.decrementAndGet(); ws.cancel(); return
            }
            if (!decoder.post statusWork@{
              if (epoch != generation.get()) {
                queued.decrementAndGet(); return@statusWork
              }
              if (nextWidth != null && nextHeight != null) {
                width = nextWidth; height = nextHeight
              }
              // Retain the queue slot until the main-thread status is handled.
              if (!post publishStatus@{
                try {
                  if (epoch != generation.get()) return@publishStatus
                  requestLayout()
                  terminalState = value in listOf("denied", "unsupported", "disconnected")
                  if (terminalState) {
                    stop(); state(value, status.optString("reason")); return@publishStatus
                  }
                  if (value == "streaming") lastFrame = android.os.SystemClock.elapsedRealtime()
                  state(value, status.optString("reason"), epoch)
                } finally { queued.decrementAndGet() }
              }) queued.decrementAndGet()
            }) queued.decrementAndGet()
          } catch (_: Exception) { ws.cancel() }
        }
        override fun onMessage(ws: WebSocket, bytes: ByteString) {
          if (epoch != generation.get()) return
          if (bytes.size > 4 * 1024 * 1024) { ws.cancel(); return }
          if (queued.incrementAndGet() > 2) {
            queued.decrementAndGet()
            // End instead of silently dropping H.264 reference frames.
            ws.cancel(); return
          }
          decoder.post {
            try { if (epoch == generation.get()) decode(bytes.toByteArray(), epoch) }
            catch (_: Exception) { ws.cancel() }
            finally { queued.decrementAndGet() }
          }
        }
        override fun onFailure(ws: WebSocket, error: Throwable, response: Response?) {
          post { if (epoch == generation.get()) { stop(); if (!terminalState) state("disconnected", "Desktop connection ended.") } }
        }
        override fun onClosing(ws: WebSocket, code: Int, reason: String) {
          post { if (epoch == generation.get()) { stop(); if (!terminalState) state("disconnected", reason) } }
        }
      })
    } catch (_: Exception) { state("disconnected", "Desktop requires a secure connection.") }
  }

  private fun decode(data: ByteArray, epoch: Int) {
    val units = AnnexB.units(data)
    val idr = units.any { (it[0].toInt() and 31) == 5 }
    if (needIDR && !idr) { dropped++; return }
    if (codec == null) {
      val sps = units.firstOrNull { (it[0].toInt() and 31) == 7 } ?: return
      val pps = units.firstOrNull { (it[0].toInt() and 31) == 8 } ?: return
      val format = MediaFormat.createVideoFormat("video/avc", width, height)
      format.setByteBuffer("csd-0", ByteBuffer.wrap(byteArrayOf(0, 0, 0, 1) + sps))
      format.setByteBuffer("csd-1", ByteBuffer.wrap(byteArrayOf(0, 0, 0, 1) + pps))
      format.setInteger(MediaFormat.KEY_MAX_INPUT_SIZE, 4 * 1024 * 1024)
      if (android.os.Build.VERSION.SDK_INT >= 30) format.setInteger(MediaFormat.KEY_LOW_LATENCY, 1)
      val created = MediaCodec.createDecoderByType("video/avc")
      codec = created
      created.configure(format, surface.holder.surface, null, 0)
      created.setOnFrameRenderedListener({ _, _, _ ->
        if (epoch == generation.get()) {
          presented++; lastFrame = android.os.SystemClock.elapsedRealtime()
          post { if (epoch == generation.get()) cover.visibility = GONE }
          if (presented == 1 || presented % 30 == 0) state("connected", epoch = epoch)
        }
      }, decoder)
      created.start()
    }
    val active = codec ?: return
    drain(active)
    val index = active.dequeueInputBuffer(10000)
    if (index < 0) throw IllegalStateException("decoder_backpressure")
    val input = active.getInputBuffer(index) ?: throw IllegalStateException("missing_input")
    if (input.capacity() < data.size) throw IllegalStateException("oversized_frame")
    input.clear(); input.put(data)
    active.queueInputBuffer(index, 0, data.size, System.nanoTime() / 1000, 0)
    needIDR = false
    drain(active)
  }

  private fun drain(active: MediaCodec) {
    val info = MediaCodec.BufferInfo()
    repeat(8) {
      val index = active.dequeueOutputBuffer(info, 0)
      if (index >= 0) active.releaseOutputBuffer(index, true)
      else if (index == MediaCodec.INFO_TRY_AGAIN_LATER) return
    }
  }

  fun sendCommand(owner: String, sequence: Int, value: String): Boolean {
    if (owner.isEmpty() || owner != inputGeneration || sequence != inputSequence + 1) return false
    if (!send(value)) return false
    inputSequence = sequence
    return true
  }

  fun disconnect(owner: String) {
    if (owner.isNotEmpty() && owner == inputGeneration) stop()
  }

  private fun send(value: String): Boolean {
    val active = socket ?: return false
    val bytes = value.toByteArray(Charsets.UTF_8).size
    if (value.isEmpty() || bytes > 8192) return false
    if (active.queueSize() + bytes > 32 * 1024 || !active.send(value)) {
      stop(); state("disconnected", "Desktop input timed out.")
      return false
    }
    return true
  }

  private fun stop() {
    generation.incrementAndGet()
    pendingConnection = ""
    inputGeneration = ""; inputSequence = 0
    removeCallbacks(heartbeat)
    socket?.cancel()
    socket = null
    lastFrame = 0
    decoder.post {
      try { codec?.stop() } catch (_: Exception) {}
      codec?.release(); codec = null; needIDR = true; presented = 0; dropped = 0
    }
    // Destroy the old surface's retained last frame on every ownership change.
    cover.visibility = VISIBLE
  }

  fun destroy() {
    if (destroyed) return
    destroyed = true; pendingConnection = ""; stop()
    decoder.post { thread.quitSafely() }
    client.dispatcher.executorService.shutdown()
    client.connectionPool.evictAll()
  }

  override fun surfaceCreated(holder: SurfaceHolder) { if (!destroyed && pendingConnection.isNotEmpty() && socket == null) open(pendingConnection) }
  override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) {}
  override fun surfaceDestroyed(holder: SurfaceHolder) { stop(); state("disconnected") }
  override fun onWindowVisibilityChanged(visibility: Int) {
    super.onWindowVisibilityChanged(visibility)
    if (visibility != VISIBLE && (socket != null || pendingConnection.isNotEmpty())) {
      stop(); state("disconnected")
    }
  }
}

internal object AnnexB {
  fun units(data: ByteArray): List<ByteArray> {
    require(data.size <= 4 * 1024 * 1024)
    val starts = mutableListOf<Pair<Int, Int>>()
    var i = 0
    while (i + 3 <= data.size) {
      val prefix = if (data[i] == 0.toByte() && data[i + 1] == 0.toByte()) {
        if (data[i + 2] == 1.toByte()) 3
        else if (i + 4 <= data.size && data[i + 2] == 0.toByte() && data[i + 3] == 1.toByte()) 4
        else 0
      } else 0
      if (prefix > 0) { starts.add(i to prefix); i += prefix } else i++
      require(starts.size <= 4096)
    }
    return starts.mapIndexedNotNull { index, (start, prefix) ->
      val end = starts.getOrNull(index + 1)?.first ?: data.size
      if (end > start + prefix) data.copyOfRange(start + prefix, end) else null
    }
  }
}
