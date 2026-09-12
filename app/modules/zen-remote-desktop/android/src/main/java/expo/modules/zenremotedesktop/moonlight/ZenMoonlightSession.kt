package expo.modules.zenremotedesktop.moonlight

import expo.modules.zenremotedesktop.MoonlightAudioConfiguration
import expo.modules.zenremotedesktop.MoonlightCore
import expo.modules.zenremotedesktop.MoonlightKeyMaterial
import java.security.SecureRandom
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import java.util.concurrent.atomic.AtomicLong

/**
 * One connected path: upstream pairing/serverinfo/launch drives the pinned C
 * core, which owns the stream and reports establishment through its callbacks.
 *
 * Cancellation/ownership: every connect attempt is bound to an epoch. stop()
 * and revoke() advance the epoch, cancel in-flight HTTP and drop the in-memory
 * pin, so a launch that returns after disconnect/revoke can never start the
 * core or write trust. No lock is held across blocking network calls.
 *
 * Trust: an existing pinned certificate is installed into the host client
 * BEFORE the first authenticated request. A corrupt or unreadable saved trust
 * is reported as a result instead of silently resetting the identity.
 *
 * Host policy (upstream NvConnection semantics): idle host -> launch; host
 * already running the requested app -> resume; a different running app is
 * reported explicitly and never terminated to manufacture a success.
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
    /** Decoder-supported VIDEO_FORMAT_* bits; never a host SCM mask. */
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
      val launchVerb: String,
      val rtspSessionUrl: String?,
      val startAccepted: Boolean,
    ) : Result()
  }

  /** Report with explicit failures; a swallowed quit/unpair is not success. */
  data class RevokeReport(
    val stopResult: Int,
    val quitSucceeded: Boolean,
    val unpairSucceeded: Boolean,
    val trustCleared: Boolean,
    val failures: List<String>,
  ) {
    /** True only when every step actually succeeded (stop included). */
    val complete: Boolean
      get() = failures.isEmpty() && stopResult == 0 && quitSucceeded && unpairSucceeded && trustCleared
  }

  private val epoch = AtomicLong(0)
  /**
   * Short admission lock: stop/revoke invalidate and connect admits core.start
   * and trust persistence under the same lock, so no check-then-act window can
   * start a session after stop returned. Never held over blocking HTTP.
   */
  private val admissionLock = Any()
  private val random = SecureRandom()

  @Volatile
  private var inMemoryPin: X509Certificate? = null

  private fun attemptAlive(attempt: Long): Boolean = epoch.get() == attempt

  /** Invalidates in-flight attempts without touching the core. */
  fun cancel(host: MoonlightHost): Long {
    val next = synchronized(admissionLock) { epoch.incrementAndGet() }
    inMemoryPin = null
    host.cancelInFlight()
    return next
  }

  fun connect(host: MoonlightHost, plan: Plan, pin: String?): Result {
    val attempt = epoch.get()

    // 1. Install existing trust before the first authenticated request.
    val saved: X509Certificate? = try {
      trustStore.load()
    } catch (error: CertificateException) {
      return Result.Rejected(null, "trust_store_corrupt")
    } catch (error: Exception) {
      return Result.Rejected(null, "trust_store_unreadable")
    }
    if (saved != null) {
      host.setServerCert(saved)
      inMemoryPin = saved
    }
    if (!attemptAlive(attempt)) {
      return Result.Rejected(null, "revoked")
    }

    // 2. Server info (pinned HTTPS when trust exists, HTTP fallback otherwise).
    val serverInfo = try {
      host.fetchServerInfo()
    } catch (error: Exception) {
      return Result.Rejected(null, if (attemptAlive(attempt)) "serverinfo_failed:${error.javaClass.simpleName}" else "revoked")
    }
    if (!attemptAlive(attempt)) {
      return Result.Rejected(serverInfo, "revoked")
    }

    // 3. Pair when the host reports it is not paired yet.
    var pairState: PairingManager.PairState? = null
    if (!serverInfo.paired()) {
      if (pin.isNullOrBlank()) {
        return Result.Rejected(serverInfo, "pairing_required")
      }
      pairState = try {
        host.pairingManager.pair(serverInfo.rawXml(), pin)
      } catch (error: Exception) {
        return Result.Rejected(serverInfo, if (attemptAlive(attempt)) "pairing_failed:${error.javaClass.simpleName}" else "revoked")
      }
      if (!attemptAlive(attempt)) {
        return Result.Rejected(serverInfo, "revoked")
      }
      if (pairState != PairingManager.PairState.PAIRED) {
        return Result.Rejected(serverInfo, "pairing_${pairState.name.lowercase()}")
      }
    }

    // 4. Persist and install the pin; never continue unpinned.
    val pinned = host.serverCert ?: saved
    if (pinned == null) {
      return Result.Rejected(serverInfo, "missing_pinned_certificate")
    }
    if (!attemptAlive(attempt)) {
      return Result.Rejected(serverInfo, "revoked")
    }
    // Persist only while this attempt still owns admission.
    synchronized(admissionLock) {
      if (epoch.get() != attempt) {
        return Result.Rejected(serverInfo, "revoked")
      }
      host.setServerCert(pinned)
      inMemoryPin = pinned
      try {
        trustStore.save(pinned)
      } catch (error: Exception) {
        return Result.Rejected(serverInfo, "trust_store_failed:${error.javaClass.simpleName}")
      }
    }

    // 5. Actual decoder-supported formats only; no SCM intersection.
    if (!MoonlightVideoFormats.isRenderable(plan.supportedVideoFormats)) {
      return Result.Rejected(serverInfo, "invalid_video_formats")
    }
    if (!MoonlightAudioConfiguration.isValid(plan.audioConfiguration)) {
      return Result.Rejected(serverInfo, "invalid_audio_configuration")
    }
    if (plan.width <= 0 || plan.height <= 0 || plan.fps <= 0) {
      return Result.Rejected(serverInfo, "invalid_resolution")
    }

    // 6. Upstream launch/resume policy. Foreign busy state is explicit.
    val verb = when {
      serverInfo.runningGameId() == 0 -> MoonlightLaunchRequest.VERB_LAUNCH
      serverInfo.runningGameId() == plan.appId -> MoonlightLaunchRequest.VERB_RESUME
      else -> return Result.Rejected(serverInfo, "host_busy_foreign_app")
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
      .verb(verb)
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
      return Result.Rejected(serverInfo, if (attemptAlive(attempt)) "launch_failed:${error.javaClass.simpleName}" else "revoked")
    }
    if (!attemptAlive(attempt)) {
      // A stop/revoke happened while the launch was in flight: never start.
      return Result.Rejected(serverInfo, "revoked")
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

    // Atomic admission: stop/revoke either invalidate before this point or
    // stop the core after it, never leave a start after stop returned.
    val startResult = synchronized(admissionLock) {
      if (epoch.get() != attempt) {
        return Result.Rejected(serverInfo, "revoked")
      }
      try {
        core.start(config)
      } catch (error: Exception) {
        return Result.Rejected(serverInfo, "start_failed:${error.javaClass.simpleName}")
      }
    }
    return Result.Started(
      serverInfo = serverInfo,
      pairState = pairState,
      launchAccepted = true,
      launchVerb = verb,
      rtspSessionUrl = outcome.rtspSessionUrl(),
      startAccepted = startResult == 0,
    )
  }

  /** Disconnect: invalidates in-flight attempts and stops the core session. */
  fun stop(host: MoonlightHost? = null): Int {
    synchronized(admissionLock) {
      epoch.incrementAndGet()
      inMemoryPin = null
    }
    if (host != null) {
      host.setServerCert(null)
      host.cancelInFlight()
    }
    return core.stop()
  }

  /**
   * Client-side revoke: invalidate attempts, drop in-memory trust, stop the
   * stream, ask the host to cancel the session and drop the pairing, and clear
   * the pinned certificate. Failures are reported, not swallowed.
   */
  fun revoke(host: MoonlightHost): RevokeReport {
    synchronized(admissionLock) {
      epoch.incrementAndGet()
    }
    host.cancelInFlight()
    val failures = mutableListOf<String>()
    val stopResult = core.stop()
    // Keep the scoped pin installed while the authenticated HTTPS quit runs;
    // only discard trust after the bounded host cleanup attempt.
    val quitSucceeded = try {
      host.quitApp()
    } catch (error: Exception) {
      failures += "quit:${error.javaClass.simpleName}"
      false
    }
    val unpairSucceeded = try {
      host.unpair()
      true
    } catch (error: Exception) {
      failures += "unpair:${error.javaClass.simpleName}"
      false
    }
    inMemoryPin = null
    host.setServerCert(null)
    val trustCleared = try {
      trustStore.clear()
      true
    } catch (error: Exception) {
      failures += "trust:${error.javaClass.simpleName}"
      false
    }
    if (stopResult != 0) {
      failures += "stop:$stopResult"
    }
    if (!quitSucceeded) {
      failures += "quit:unsuccessful"
    }
    if (!unpairSucceeded) {
      failures += "unpair:unsuccessful"
    }
    return RevokeReport(stopResult, quitSucceeded, unpairSucceeded, trustCleared, failures)
  }

  /** Last pin installed in memory; cleared by stop/revoke. */
  fun inMemoryPin(): X509Certificate? = inMemoryPin
}
