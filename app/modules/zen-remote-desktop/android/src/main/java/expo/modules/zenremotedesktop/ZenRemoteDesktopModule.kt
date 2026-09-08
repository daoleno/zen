package expo.modules.zenremotedesktop

import android.content.Context
import android.media.MediaCodec
import android.media.MediaFormat
import android.os.Handler
import android.os.HandlerThread
import android.util.Log
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
import expo.modules.zenlinktransport.PinnedEndpointRegistry
import android.app.AlertDialog
import android.text.InputType
import android.view.WindowManager
import android.view.inputmethod.EditorInfo
import android.widget.EditText
import org.json.JSONArray

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
      AsyncFunction("showSensitiveInput") { view: DesktopView, generation: String -> view.showSensitiveInput(generation) }
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
  private val admission = DecodeAdmission()
  private val generation = AtomicInteger(0)
  private val client = OkHttpClient.Builder().readTimeout(0, TimeUnit.MILLISECONDS)
    .followRedirects(false).followSslRedirects(false)
    .connectTimeout(10, TimeUnit.SECONDS).writeTimeout(2, TimeUnit.SECONDS).build()
  private var socket: WebSocket? = null
  @Volatile private var pendingConnection = ""
  private var inputGeneration = ""
  private var inputSequence = 0
  private var selectedSource = ""
  @Volatile private var hostControl = false
  @Volatile private var sensitiveReady = false
  @Volatile private var tlsConnected = false
  @Volatile private var hostSurface = ""
  private var sensitiveDialog: AlertDialog? = null
  private var sensitiveEditor: EditText? = null
  private var codec: MediaCodec? = null
  private var width = 1280
  private var height = 720
  private var needIDR = true
  @Volatile private var presented = 0
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
    if (context.applicationInfo.flags and android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE != 0) {
      Log.d("ZenDesktop", "state=$value generation=$epoch presented=$presented dropped=$dropped size=${width}x$height reason=$reason")
    }
    val event = mapOf("state" to value, "reason" to reason, "source" to selectedSource, "width" to width, "height" to height, "presented" to presented, "dropped" to dropped, "control" to hostControl, "surface" to hostSurface, "sensitiveInput" to (sensitiveReady && encryptedTransport()))
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
      require(DesktopTransportPolicy.allows(url, config.getString("transport"), config.getString("boundOrigin"),
        config.getString("sourceOrigin"), config.optString("transportPin")) { host ->
        try {
          (android.system.Os.inet_pton(android.system.OsConstants.AF_INET, host)
            ?: android.system.Os.inet_pton(android.system.OsConstants.AF_INET6, host))?.address
        } catch (_: Exception) { null }
      })
      val request = Request.Builder().url(url).header("Authorization", config.getString("authorization")).build()
      socket = client.newWebSocket(request, object : WebSocketListener() {
        override fun onOpen(ws: WebSocket, response: Response) {
          if (epoch != generation.get()) { ws.cancel(); return }
          post { if (epoch == generation.get()) { tlsConnected = response.handshake != null; removeCallbacks(heartbeat); post(heartbeat) } }
        }
        override fun onMessage(ws: WebSocket, text: String) {
          if (epoch != generation.get()) return
          if (text.toByteArray(Charsets.UTF_8).size > 8192) { ws.cancel(); return }
          try {
            val status = JSONObject(text)
            // Validate on the receiving thread before posting asynchronous work.
            val value = status.getString("state")
            require(value in listOf("sources", "requesting", "streaming", "denied", "unsupported", "disconnected"))
            val nextSource = if (value == "sources") {
              val sources = status.getJSONArray("sources")
              require(sources.length() == 1)
              sources.getJSONObject(0).getString("id").also { require(it in listOf("x11", "wayland")) }
            } else null
            require(status.has("width") == status.has("height"))
            val nextWidth = if (status.has("width")) status.getInt("width") else null
            val nextHeight = if (status.has("height")) status.getInt("height") else null
            if (nextWidth != null && nextHeight != null) {
              require(nextWidth in 2..4096 && nextHeight in 2..4096)
            }
            if (!admission.acquire({ epoch == generation.get() })) {
              Log.w("ZenDesktop", "Status admission timed out or generation ended")
              ws.cancel(); return
            }
            if (!decoder.post statusWork@{
              if (epoch != generation.get()) {
                admission.release(); return@statusWork
              }
              if (nextWidth != null && nextHeight != null) {
                width = nextWidth; height = nextHeight
              }
              // Retain the queue slot until the main-thread status is handled.
              if (!post publishStatus@{
                try {
                  if (epoch != generation.get()) return@publishStatus
                  if (nextSource != null) selectedSource = nextSource
                  if (value == "streaming") {
                    hostSurface = status.optString("surface", "desktop")
                    hostControl = status.optBoolean("control", false)
                    sensitiveReady = status.optBoolean("sensitiveInput", false) && hostControl
                  }
                  requestLayout()
                  terminalState = value in listOf("denied", "unsupported", "disconnected")
                  if (terminalState) {
                    stop(); state(value, status.optString("reason")); return@publishStatus
                  }
                  if (value == "streaming") lastFrame = android.os.SystemClock.elapsedRealtime()
                  state(value, status.optString("reason"), epoch)
                } finally { admission.release() }
              }) admission.release()
            }) admission.release()
          } catch (_: Exception) { ws.cancel() }
        }
        override fun onMessage(ws: WebSocket, bytes: ByteString) {
          if (epoch != generation.get()) return
          if (bytes.size > 4 * 1024 * 1024) { ws.cancel(); return }
          // Backpressure the socket reader during codec startup without dropping reference frames.
          if (!admission.acquire({ epoch == generation.get() })) {
            Log.w("ZenDesktop", "Decoder admission timed out or generation ended")
            ws.cancel(); return
          }
          if (!decoder.post {
            try { if (epoch == generation.get()) decode(bytes.toByteArray(), epoch) }
            catch (error: Exception) { Log.w("ZenDesktop", "Decoder failed", error); ws.cancel() }
            finally { admission.release() }
          }) admission.release()
        }
        override fun onFailure(ws: WebSocket, error: Throwable, response: Response?) {
          post { if (epoch == generation.get()) {
            val denied = response?.code == 401 || response?.code == 403
            stop()
            if (!terminalState) state(if (denied) "denied" else "disconnected", if (denied) "Desktop authorization is required." else "Desktop connection ended.")
          } }
        }
        override fun onClosing(ws: WebSocket, code: Int, reason: String) {
          post { if (epoch == generation.get()) { stop(); if (!terminalState) state("disconnected", reason) } }
        }
      })
    } catch (_: Exception) { state("disconnected", "Desktop transport approval is missing or invalid.") }
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
      Log.d("ZenDesktop", "decoder=${created.name} size=${width}x$height")
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
    val index = awaitDecoderInput({ epoch == generation.get() }, active::dequeueInputBuffer, { drain(active) })
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

  private fun encryptedTransport(): Boolean {
    return try {
      val config = JSONObject(pendingConnection)
      when (config.getString("transport")) {
        "tls" -> tlsConnected
        "pinned-link" -> PinnedEndpointRegistry.contains(java.net.URI(config.getString("url")).port, config.getString("transportPin"))
        else -> false
      }
    } catch (_: Exception) { false }
  }

  fun showSensitiveInput(owner: String): Boolean {
    if (owner.isEmpty() || owner != inputGeneration || presented < 1 || !sensitiveReady || !encryptedTransport() || sensitiveDialog != null) return false
    val activity = appContext.currentActivity ?: return false
    val epoch = generation.get()
    val editor = object : EditText(activity) {
      override fun onTextContextMenuItem(id: Int): Boolean = false
    }.apply {
      setSingleLine(true)
      inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
      transformationMethod = android.text.method.PasswordTransformationMethod.getInstance()
      imeOptions = EditorInfo.IME_FLAG_NO_PERSONALIZED_LEARNING or EditorInfo.IME_ACTION_DONE
      isSaveEnabled = false
      setFreezesText(false)
      isLongClickable = false
      if (android.os.Build.VERSION.SDK_INT >= 26) importantForAutofill = IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS
      filters = arrayOf(android.text.InputFilter.LengthFilter(64))
    }
    val dialog = AlertDialog.Builder(activity).setTitle("OS password").setView(editor)
      .setNegativeButton("Cancel", null).setPositiveButton("Submit", null).create()
    sensitiveEditor = editor
    sensitiveDialog = dialog
    dialog.setOnDismissListener {
      editor.text?.clear()
      if (sensitiveDialog === dialog) { sensitiveDialog = null; sensitiveEditor = null }
    }
    dialog.setOnShowListener {
      dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
        if (epoch != generation.get() || owner != inputGeneration || !sensitiveReady || !encryptedTransport()) { dialog.dismiss(); return@setOnClickListener }
        val value = editor.text
        if (value.isNullOrEmpty() || value.any { it.code !in 0x20..0x7e }) { editor.error = "Unsupported character."; return@setOnClickListener }
        val events = JSONArray()
        for (char in value) events.put(JSONObject().put("type", "text").put("code", char.code))
        val payload = JSONObject().put("type", "sensitive").put("events", events).put("submit", true).toString()
        value.clear()
        send(payload)
        dialog.dismiss()
      }
      editor.requestFocus()
    }
    dialog.window?.addFlags(WindowManager.LayoutParams.FLAG_SECURE)
    dialog.show()
    return true
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
    sensitiveEditor?.text?.clear()
    sensitiveDialog?.dismiss()
    sensitiveDialog = null; sensitiveEditor = null
    sensitiveReady = false; hostControl = false; tlsConnected = false
    hostSurface = ""
    pendingConnection = ""
    inputGeneration = ""; inputSequence = 0
    selectedSource = ""
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
