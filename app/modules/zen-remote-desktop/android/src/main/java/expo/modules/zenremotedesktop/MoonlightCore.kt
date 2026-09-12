package expo.modules.zenremotedesktop

import java.security.SecureRandom
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Thin Kotlin contract over the pinned moonlight-common-c core
 * (app/modules/zen-remote-desktop/native.lock.json).
 *
 * The C bridge owns the session state machine. `LiStartConnection` returns as
 * soon as the stream is established (the pinned Connection.c calls
 * connectionStarted and returns), so a successful [start] is followed by live
 * callbacks until termination or [stop]. [stop] asks the bridge for the single
 * real `LiStopConnection` teardown; it is safe to call while a start is in
 * flight (the bridge interrupts and waits) and from any callback.
 */
open class MoonlightCore {

  data class Config(
    val address: String,
    val appVersion: String,
    val gfeVersion: String,
    val rtspSessionUrl: String,
    val serverCodecModeSupport: Int,
    val width: Int,
    val height: Int,
    val fps: Int,
    val bitrateKbps: Int,
    val packetSize: Int,
    val audioConfiguration: Int,
    val supportedVideoFormats: Int,
    val clientRefreshRateX100: Int,
    val streamingRemotely: Int,
    /** Client-generated RI key. Exactly 16 bytes; never logged. */
    val remoteInputAesKey: ByteArray,
    /** Client-generated RI IV. Exactly 16 bytes; never logged. */
    val remoteInputAesIv: ByteArray,
  )

  private val startThreadLock = Any()

  @Volatile
  private var startThread: Thread? = null

  /** Called from the core once video dimensions/format are negotiated. Return 0 to accept. */
  open fun onVideoSetup(videoFormat: Int, width: Int, height: Int, redrawRate: Int): Int = 0

  /** Progress for the current connection stage. */
  open fun onConnectionStage(stage: Int) {}

  open fun onConnectionStageComplete(stage: Int) {}

  open fun onConnectionStageFailed(stage: Int, errorCode: Int) {}

  open fun onConnectionStarted() {}

  /**
   * Async start failure surfaced by the bridge (for example a setup or stage
   * callback rejected the stream). Not called for an intentional stop.
   */
  open fun onConnectionStartFailed(errorCode: Int) {}

  /** CONN_STATUS_OKAY (0) or CONN_STATUS_POOR (1). */
  open fun onConnectionStatusUpdate(connectionStatus: Int) {}

  open fun onConnectionTerminated(errorCode: Int) {}

  /**
   * Annex B access unit. Return 0 to accept or -1 to request a keyframe.
   * The array is copied by the bridge from core-owned buffers and is only valid
   * for the duration of this call.
   */
  open fun onDecodeUnit(data: ByteArray, frameType: Int, presentationTimeUs: Long): Int = 0

  /**
   * Starts a session. Returns 0 when the start call was accepted, -2 when a
   * session is already running, -3 on a bridge state error and -4 when the
   * negotiated key material is missing or not exactly 16 bytes per key/IV.
   */
  fun start(config: Config): Int {
    // Fail fast before any thread or native session exists; the bridge repeats
    // every check authoritatively before it touches the core.
    if (!MoonlightKeyMaterial.isValid(config.remoteInputAesKey, config.remoteInputAesIv)) {
      return -4
    }
    if (!validStreamArguments(config)) {
      return -5
    }
    if (sessionState() and 0x0F != SESSION_IDLE) {
      return -2
    }
    synchronized(startThreadLock) {
      if (startThread?.isAlive == true) {
        return -2
      }
      val thread = Thread({
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
      }, "zen-moonlight-start")
      startThread = thread
      thread.start()
      return 0
    }
  }

  /**
   * Requests the single bridge-managed teardown. Returns 0 on success, -3 when
   * this instance does not own the live session. Safe while starting or idle.
   */
  fun stop(): Int = nativeStopConnection()

  /** Bridge session state plus termination/callback-failure flags. */
  fun sessionState(): Int = nativeSessionState()

  /** Last connectionTerminated error code, 0 when none was observed. */
  fun lastError(): Int = nativeLastError()

  /** Last failed LiStartConnection result, 0 when the last start succeeded. */
  fun lastStartError(): Int = nativeLastStartError()

  fun isActive(): Boolean = nativeIsActive()

  fun sendKeyboardEvent(keyCode: Short, keyAction: Byte, modifiers: Byte, flags: Byte = 0): Int =
    nativeSendKeyboardEvent(keyCode, keyAction, modifiers, flags)

  /**
   * Sends a text event as standard UTF-8 bytes. The bridge rejects empty,
   * oversized or non-owner sends; text is never logged.
   */
  fun sendUtf8TextEvent(text: String): Int {
    val bytes = text.toByteArray(Charsets.UTF_8)
    if (bytes.isEmpty()) {
      return -5
    }
    return nativeSendUtf8TextEvent(bytes)
  }

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
    remoteInputAesKey: ByteArray,
    remoteInputAesIv: ByteArray,
  ): Int

  private external fun nativeStopConnection(): Int

  private external fun nativeInterruptConnection()

  private external fun nativeIsActive(): Boolean

  private external fun nativeSessionState(): Int

  private external fun nativeLastError(): Int

  private external fun nativeLastStartError(): Int

  private external fun nativeSendKeyboardEvent(keyCode: Short, keyAction: Byte, modifiers: Byte, flags: Byte): Int

  private external fun nativeSendUtf8TextEvent(utf8: ByteArray): Int

  private external fun nativeSendMouseMove(deltaX: Short, deltaY: Short): Int

  private external fun nativeSendMousePosition(x: Short, y: Short, referenceWidth: Short, referenceHeight: Short): Int

  private external fun nativeSendMouseButton(buttonAction: Byte, button: Int): Int

  private external fun nativeSendScroll(scrollClicks: Byte): Int

  private external fun nativeSendHighResScroll(scrollAmount: Short): Int

  companion object {
    private fun validStreamArguments(config: Config): Boolean =
      config.address.isNotEmpty() &&
        config.serverCodecModeSupport != 0 &&
        MoonlightAudioConfiguration.isValid(config.audioConfiguration) &&
        config.supportedVideoFormats != 0 &&
        config.width in 16..8192 &&
        config.height in 16..8192 &&
        config.fps in 1..240 &&
        config.bitrateKbps in 100..200_000 &&
        config.packetSize in 64..65_536

    /** Session states reported by the bridge. */
    const val SESSION_IDLE = 0
    const val SESSION_STARTING = 1
    const val SESSION_ACTIVE = 2
    const val SESSION_STOPPING = 3
    const val STATE_FLAG_TERMINATED = 0x10
    const val STATE_FLAG_CALLBACK_FAILED = 0x20

    // Upstream constants used by callers; kept in sync with src/Limelight.h.
    // VIDEO_FORMAT_* are client decoder bits; SCM_* are host codec-mode bits.
    // They are different masks (AV1_MAIN8 is 0x1000 vs SCM_AV1_MAIN8 0x10000).
    const val VIDEO_FORMAT_H264 = 0x0001
    const val VIDEO_FORMAT_H264_HIGH8_444 = 0x0004
    const val VIDEO_FORMAT_H265 = 0x0100
    const val VIDEO_FORMAT_H265_MAIN10 = 0x0200
    const val VIDEO_FORMAT_H265_REXT8_444 = 0x0400
    const val VIDEO_FORMAT_H265_REXT10_444 = 0x0800
    const val VIDEO_FORMAT_AV1_MAIN8 = 0x1000
    const val VIDEO_FORMAT_AV1_MAIN10 = 0x2000
    const val VIDEO_FORMAT_AV1_HIGH8_444 = 0x4000
    const val VIDEO_FORMAT_AV1_HIGH10_444 = 0x8000
    const val VIDEO_FORMAT_MASK_H264 = 0x000F
    const val VIDEO_FORMAT_MASK_H265 = 0x0F00
    const val VIDEO_FORMAT_MASK_AV1 = 0xF000
    const val VIDEO_FORMAT_MASK_10BIT = 0xAA00
    const val VIDEO_FORMAT_MASK_YUV444 = 0xCC04

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
    const val SERVER_CODEC_MODE_HEVC_MAIN10 = 0x00000200
    const val SERVER_CODEC_MODE_AV1_MAIN8 = 0x00010000
    const val SERVER_CODEC_MODE_AV1_MAIN10 = 0x00020000

    /** Non-null when the production native library could not be loaded upfront. */
    @Volatile
    var nativeLoadFailure: Throwable? = null
      private set

    init {
      try {
        System.loadLibrary("zen_moonlight")
      } catch (error: UnsatisfiedLinkError) {
        // Unit tests load a host-built bridge explicitly before first use.
        nativeLoadFailure = error
      }
    }
  }
}

/** Client-side RI key material. Keys are never persisted or logged here. */
object MoonlightKeyMaterial {
  private val random = SecureRandom()

  fun generateKey(): ByteArray = ByteArray(16).also(random::nextBytes)

  fun generateIv(): ByteArray = ByteArray(16).also(random::nextBytes)

  fun isValid(key: ByteArray?, iv: ByteArray?): Boolean =
    key != null && iv != null && key.size == 16 && iv.size == 16 && key.any { it != 0.toByte() }
}

/**
 * Packed audio configuration using the same layout as upstream
 * MAKE_AUDIO_CONFIGURATION: magic 0xCA, channel count in bits 8..15, channel
 * mask in bits 16..31.
 */
object MoonlightAudioConfiguration {
  const val STEREO = (0x3 shl 16) or (2 shl 8) or 0xCA
  const val SURROUND_51 = (0x3F shl 16) or (6 shl 8) or 0xCA
  const val SURROUND_71 = (0x63F shl 16) or (8 shl 8) or 0xCA

  fun channelCount(value: Int): Int = (value shr 8) and 0xFF

  fun channelMask(value: Int): Int = (value shr 16) and 0xFFFF

  fun isValid(value: Int): Boolean = value and 0xFF == 0xCA && channelCount(value) in 1..8
}
