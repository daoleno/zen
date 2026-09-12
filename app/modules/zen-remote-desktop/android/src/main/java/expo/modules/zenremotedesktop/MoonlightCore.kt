package expo.modules.zenremotedesktop

import java.util.concurrent.atomic.AtomicBoolean

/**
 * Thin Kotlin contract over the pinned moonlight-common-c core
 * (app/modules/zen-remote-desktop/native.lock.json).
 *
 * The C core owns the stream/control protocol. This class owns only the
 * upstream lifecycle rules:
 *
 *  - `LiStartConnection` blocks until the session ends, so it runs on a
 *    dedicated thread created here.
 *  - to end a live session callers use [stop], which first interrupts
 *    (thread-safe) and then joins the start thread before re-arming.
 *  - decode units are delivered on core threads and must be consumed
 *    synchronously; the byte array is copied by the C bridge and remains
 *    owned by this process afterwards.
 */
open class MoonlightCore {

  data class Config(
    val address: String,
    val appVersion: String = "",
    val gfeVersion: String = "",
    val rtspSessionUrl: String = "",
    val serverCodecModeSupport: Int,
    val width: Int,
    val height: Int,
    val fps: Int,
    val bitrateKbps: Int,
    val packetSize: Int = 1024,
    val audioConfiguration: Int,
    val supportedVideoFormats: Int,
    val clientRefreshRateX100: Int = 0,
    val streamingRemotely: Int = -1,
    val remoteInputAesKey: ByteArray? = null,
    val remoteInputAesIv: ByteArray? = null,
  )

  private val running = AtomicBoolean(false)

  @Volatile
  private var startThread: Thread? = null

  /** Called from the core once video dimensions/format are negotiated. Return 0 to accept. */
  open fun onVideoSetup(videoFormat: Int, width: Int, height: Int, redrawRate: Int): Int = 0

  /** Progress for the current connection stage. */
  open fun onConnectionStage(stage: Int) {}

  open fun onConnectionStageFailed(stage: Int, errorCode: Int) {}

  open fun onConnectionStarted() {}

  open fun onConnectionTerminated(errorCode: Int) {}

  /**
   * Annex B access unit. Return 0 to accept or -1 to request a keyframe.
   * The array is only valid for the duration of this call.
   */
  open fun onDecodeUnit(data: ByteArray, frameType: Int, presentationTimeUs: Long): Int = 0

  /**
   * Starts a session. Returns 0 when the start thread was launched, -2 when a
   * session is already running, -3 on a bridge state error.
   */
  fun start(config: Config): Int {
    if (!running.compareAndSet(false, true)) {
      return -2
    }
    val thread = Thread({
      try {
        nativeStartConnection(
          address = config.address,
          appVersion = config.appVersion,
          gfeVersion = config.gfeVersion,
          rtspSessionUrl = config.rtspSessionUrl,
          serverCodecModeSupport = config.serverCodecModeSupport,
          width = config.width,
          height = config.height,
          fps = config.fps,
          bitrate = config.bitrateKbps,
          packetSize = config.packetSize,
          audioConfiguration = config.audioConfiguration,
          supportedVideoFormats = config.supportedVideoFormats,
          clientRefreshRateX100 = config.clientRefreshRateX100,
          streamingRemotely = config.streamingRemotely,
          remoteInputAesKey = config.remoteInputAesKey,
          remoteInputAesIv = config.remoteInputAesIv,
        )
      } finally {
        running.set(false)
      }
    }, "zen-moonlight-start")
    startThread = thread
    thread.start()
    return 0
  }

  /** Interrupts and joins the start thread. Safe to call when idle. */
  fun stop() {
    nativeInterruptConnection()
    startThread?.let { thread ->
      thread.join(5_000)
      if (thread.isAlive) {
        return
      }
    }
    startThread = null
    nativeStopConnection()
  }

  fun isActive(): Boolean = nativeIsActive()

  fun sendKeyboardEvent(keyCode: Short, keyAction: Byte, modifiers: Byte, flags: Byte = 0): Int =
    nativeSendKeyboardEvent(keyCode, keyAction, modifiers, flags)

  fun sendUtf8TextEvent(text: String): Int = nativeSendUtf8TextEvent(text)

  fun sendMouseMove(deltaX: Short, deltaY: Short): Int = nativeSendMouseMove(deltaX, deltaY)

  fun sendMousePosition(x: Short, y: Short, referenceWidth: Short, referenceHeight: Short): Int =
    nativeSendMousePosition(x, y, referenceWidth, referenceHeight)

  fun sendMouseButton(buttonAction: Byte, button: Int): Int = nativeSendMouseButton(buttonAction, button)

  /** Signed click count; positive scrolls up. */
  fun sendScroll(scrollClicks: Byte): Int = nativeSendScroll(scrollClicks)

  fun sendHighResScroll(scrollAmount: Short): Int = nativeSendHighResScroll(scrollAmount)

  private external fun nativeStartConnection(
    address: String,
    appVersion: String,
    gfeVersion: String,
    rtspSessionUrl: String,
    serverCodecModeSupport: Int,
    width: Int,
    height: Int,
    fps: Int,
    bitrate: Int,
    packetSize: Int,
    audioConfiguration: Int,
    supportedVideoFormats: Int,
    clientRefreshRateX100: Int,
    streamingRemotely: Int,
    remoteInputAesKey: ByteArray?,
    remoteInputAesIv: ByteArray?,
  ): Int

  private external fun nativeStopConnection()

  private external fun nativeInterruptConnection()

  private external fun nativeIsActive(): Boolean

  private external fun nativeSendKeyboardEvent(keyCode: Short, keyAction: Byte, modifiers: Byte, flags: Byte): Int

  private external fun nativeSendUtf8TextEvent(text: String): Int

  private external fun nativeSendMouseMove(deltaX: Short, deltaY: Short): Int

  private external fun nativeSendMousePosition(x: Short, y: Short, referenceWidth: Short, referenceHeight: Short): Int

  private external fun nativeSendMouseButton(buttonAction: Byte, button: Int): Int

  private external fun nativeSendScroll(scrollClicks: Byte): Int

  private external fun nativeSendHighResScroll(scrollAmount: Short): Int

  companion object {
    init {
      System.loadLibrary("zen_moonlight")
    }

    // Upstream constants used by callers; kept in sync with src/Limelight.h.
    const val VIDEO_FORMAT_H264 = 0x0001
    const val VIDEO_FORMAT_H265 = 0x0100
    const val VIDEO_FORMAT_H265_MAIN10 = 0x0200
    const val VIDEO_FORMAT_AV1_MAIN8 = 0x10000

    const val FRAME_TYPE_PFRAME = 0x00
    const val FRAME_TYPE_IDR = 0x01

    const val KEY_ACTION_DOWN = 0x03.toByte()
    const val KEY_ACTION_UP = 0x04.toByte()

    const val BUTTON_ACTION_PRESS = 0x07.toByte()
    const val BUTTON_ACTION_RELEASE = 0x08.toByte()
    const val BUTTON_LEFT = 1
    const val BUTTON_MIDDLE = 2
    const val BUTTON_RIGHT = 3

    const val SERVER_CODEC_MODE_H264 = 0x00000001
    const val SERVER_CODEC_MODE_HEVC = 0x00000100
  }
}
