package expo.modules.zenremotedesktop.moonlight

import expo.modules.zenremotedesktop.MoonlightAudioConfiguration
import expo.modules.zenremotedesktop.MoonlightCore
import expo.modules.zenremotedesktop.MoonlightKeyMaterial
import java.security.SecureRandom

/**
 * One connected path: upstream pairing/serverinfo/launch drives the pinned C
 * core, which owns the stream and reports establishment through its callbacks.
 *
 * Provider identity, host addresses and ports are constructed by the caller
 * from Zen-scoped storage; this class only sequences the upstream application
 * layer and the core, persists the paired certificate and hands the negotiated
 * RI key material to the core. `Started.startAccepted` means the bridge accepted
 * the start call; `connectionStarted` from the core is the establishment proof.
 */
class ZenMoonlightSession(
  private val trustStore: ZenHostTrustStore,
  private val core: CoreControl,
) {

  interface CoreControl {
    fun start(config: MoonlightCore.Config): Int

    fun stop(): Int
  }

  data class Plan(
    val appId: Int,
    val width: Int,
    val height: Int,
    val fps: Int,
    val bitrateKbps: Int,
    val packetSize: Int,
    val audioConfiguration: Int,
    val supportedVideoFormats: Int,
    val streamingRemotely: Int = -1,
    val enableSops: Boolean = true,
    val remoteControllersBitmap: Int = 0,
    val attachedGamepadMask: Int = 0,
  )

  sealed class Result {
    data class Rejected(val serverInfo: MoonlightServerInfo?, val reason: String) : Result()

    data class Started(
      val serverInfo: MoonlightServerInfo,
      val pairState: PairingManager.PairState?,
      val launchAccepted: Boolean,
      val rtspSessionUrl: String?,
      val startAccepted: Boolean,
    ) : Result()
  }

  private val random = SecureRandom()

  fun connect(host: MoonlightHost, plan: Plan, pin: String?): Result {
    val serverInfo = try {
      host.fetchServerInfo()
    } catch (error: Exception) {
      return Result.Rejected(null, "serverinfo_failed:${error.javaClass.simpleName}")
    }

    var pairState: PairingManager.PairState? = null
    if (!serverInfo.paired()) {
      if (pin.isNullOrBlank()) {
        return Result.Rejected(serverInfo, "pairing_required")
      }
      pairState = try {
        host.pairingManager.pair(serverInfo.rawXml(), pin)
      } catch (error: Exception) {
        return Result.Rejected(serverInfo, "pairing_failed:${error.javaClass.simpleName}")
      }
      if (pairState != PairingManager.PairState.PAIRED) {
        return Result.Rejected(serverInfo, "pairing_${pairState.name.lowercase()}")
      }
    }

    // A paired host must have a pinned certificate; never continue unpinned.
    val pinned = host.serverCert ?: trustStore.load()
    if (pinned == null) {
      return Result.Rejected(serverInfo, "missing_pinned_certificate")
    }
    try {
      trustStore.save(pinned)
    } catch (error: Exception) {
      return Result.Rejected(serverInfo, "trust_store_failed:${error.javaClass.simpleName}")
    }

    // Actual supported formats: the plan must intersect the host's codec mask.
    if (!MoonlightAudioConfiguration.isValid(plan.audioConfiguration)) {
      return Result.Rejected(serverInfo, "invalid_audio_configuration")
    }
    if (plan.supportedVideoFormats and serverInfo.serverCodecModeSupport().toInt() == 0) {
      return Result.Rejected(serverInfo, "no_common_video_format")
    }
    if (plan.width <= 0 || plan.height <= 0 || plan.fps <= 0) {
      return Result.Rejected(serverInfo, "invalid_resolution")
    }

    val riKey = MoonlightKeyMaterial.generateKey()
    val riKeyId = random.nextInt()
    // Upstream NvConnection: 16-byte IV with the RI key id as a big-endian int
    // in the first four bytes; the core treats it as the input AES IV.
    val riIv = ByteArray(16)
    riIv[0] = (riKeyId ushr 24).toByte()
    riIv[1] = (riKeyId ushr 16).toByte()
    riIv[2] = (riKeyId ushr 8).toByte()
    riIv[3] = riKeyId.toByte()

    val surroundAudioInfo = (MoonlightAudioConfiguration.channelMask(plan.audioConfiguration) shl 16) or
      MoonlightAudioConfiguration.channelCount(plan.audioConfiguration)

    val request = MoonlightLaunchRequest.Builder()
      .verb(MoonlightLaunchRequest.VERB_LAUNCH)
      .appId(plan.appId)
      .resolution(plan.width, plan.height, plan.fps)
      .riKey(riKey, riKeyId)
      .audio(surroundAudioInfo, false)
      .controllers(plan.remoteControllersBitmap, plan.attachedGamepadMask, false)
      .sops(plan.enableSops)
      .build()

    val outcome = try {
      host.launchOrResume(request)
    } catch (error: Exception) {
      return Result.Rejected(serverInfo, "launch_failed:${error.javaClass.simpleName}")
    }
    if (!outcome.accepted()) {
      return Result.Rejected(serverInfo, "launch_rejected")
    }

    val config = MoonlightCore.Config(
      address = host.address(),
      appVersion = serverInfo.appVersion(),
      gfeVersion = serverInfo.gfeVersion() ?: "",
      rtspSessionUrl = outcome.rtspSessionUrl() ?: "",
      serverCodecModeSupport = serverInfo.serverCodecModeSupport().toInt(),
      width = plan.width,
      height = plan.height,
      fps = plan.fps,
      bitrateKbps = plan.bitrateKbps,
      packetSize = plan.packetSize,
      audioConfiguration = plan.audioConfiguration,
      supportedVideoFormats = plan.supportedVideoFormats,
      clientRefreshRateX100 = 0,
      streamingRemotely = plan.streamingRemotely,
      remoteInputAesKey = riKey,
      remoteInputAesIv = riIv,
    )

    val startResult = core.start(config)
    return Result.Started(
      serverInfo = serverInfo,
      pairState = pairState,
      launchAccepted = true,
      rtspSessionUrl = outcome.rtspSessionUrl(),
      startAccepted = startResult == 0,
    )
  }

  /** Stops the core session. Host-enforced revoke is [revoke]. */
  fun stop(): Int = core.stop()

  /**
   * Client-side revoke: stop the stream, ask the host to cancel the session and
   * drop the pairing, and clear the pinned certificate so the next connection
   * must pair again. Host-enforced termination remains the host's authority.
   */
  fun revoke(host: MoonlightHost): Int {
    val stopResult = core.stop()
    runCatching { host.quitApp() }
    runCatching { host.unpair() }
    runCatching { trustStore.clear() }
    return stopResult
  }
}
