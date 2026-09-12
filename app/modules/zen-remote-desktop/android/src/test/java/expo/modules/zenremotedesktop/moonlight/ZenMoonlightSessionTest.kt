package expo.modules.zenremotedesktop.moonlight

import expo.modules.zenremotedesktop.MoonlightAudioConfiguration
import expo.modules.zenremotedesktop.MoonlightCore
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.io.IOException
import java.nio.file.Files
import java.security.cert.X509Certificate

/**
 * Production-boundary regressions for the four Review1121 defects plus the
 * connected-path orchestration contract. No network is used; the concrete
 * MoonlightNvHttp trust manager is still exercised directly.
 */
class ZenMoonlightSessionTest {

  private class RecordingCore : ZenMoonlightSession.CoreControl {
    val configs = mutableListOf<MoonlightCore.Config>()
    var startResult = 0
    var stopResult = 0
    var stopCalls = 0

    override fun start(config: MoonlightCore.Config): Int {
      configs += config
      return startResult
    }

    override fun stop(): Int {
      stopCalls++
      return stopResult
    }
  }

  private class StubPairingManager(
    crypto: LimelightCryptoProvider,
    private val host: FakeHost,
  ) : PairingManager(FakePairingHttp, crypto) {
    var state = PairingManager.PairState.PAIRED
    var pairCalls = 0

    override fun pair(serverInfo: String, pin: String): PairingManager.PairState {
      pairCalls++
      host.setServerCert(host.expectedPinnedCert)
      return state
    }
  }

  private object FakePairingHttp : MoonlightPairingHttp {
    override fun getServerMajorVersion(serverInfo: String): Int = 7
    override fun executePairingCommand(additionalArguments: String, enableReadTimeout: Boolean): String =
      throw UnsupportedOperationException("stub")

    override fun executePairingChallenge(): String = throw UnsupportedOperationException("stub")
    override fun unpair() {}
    override fun setServerCert(serverCert: X509Certificate?) {}
  }

  private class FakeHost(var info: MoonlightServerInfo) : MoonlightHost {
    private var pinnedCert: X509Certificate? = null
    var expectedPinnedCert: X509Certificate? = null
    var launchOutcome = MoonlightHost.LaunchOutcome(true, "rtsp://192.0.2.10:48010")
    var lastRequest: MoonlightLaunchRequest? = null
    val events = mutableListOf<String>()
    var fetchCalls = 0
    var quitCalls = 0
    var unpairCalls = 0
    var cancelCalls = 0
    var quitFailure = false
    var unpairFailure = false
    var pinAtQuit: X509Certificate? = null
    var onLaunch: (() -> Unit)? = null
    lateinit var pairing: StubPairingManager

    override fun address(): String = "192.0.2.10"

    override fun getServerCert(): X509Certificate? = pinnedCert

    override fun setServerCert(serverCert: X509Certificate?) {
      pinnedCert = serverCert
      events += "pin"
    }

    override fun fetchServerInfo(): MoonlightServerInfo {
      fetchCalls++
      events += "fetch"
      return info
    }

    override fun getPairingManager(): PairingManager = pairing

    override fun launchOrResume(request: MoonlightLaunchRequest): MoonlightHost.LaunchOutcome {
      lastRequest = request
      events += "launch"
      onLaunch?.invoke()
      return launchOutcome
    }

    override fun quitApp(): Boolean {
      quitCalls++
      pinAtQuit = pinnedCert
      if (quitFailure) throw IOException("quit failed")
      return true
    }

    override fun unpair() {
      unpairCalls++
      if (unpairFailure) throw IOException("unpair failed")
    }

    override fun cancelInFlight() {
      cancelCalls++
    }
  }

  private class Fixture {
    val dir: File = Files.createTempDirectory("zen-session-test").toFile()
    val crypto = ZenCryptoProvider(dir)
    val trustStore = ZenHostTrustStore(dir)
    val core = RecordingCore()
    val host: FakeHost
    val session: ZenMoonlightSession

    init {
      host = FakeHost(unpairedInfo())
      host.expectedPinnedCert = crypto.clientCertificate
      host.pairing = StubPairingManager(crypto, host)
      session = ZenMoonlightSession(trustStore, core)
    }

    fun plan(
      supportedFormats: Int = MoonlightVideoFormats.DECODER_SUPPORTED,
      audioConfiguration: Int = MoonlightAudioConfiguration.STEREO,
      width: Int = 1920,
      appId: Int = 1,
    ) = ZenMoonlightSession.Plan(
      appId = appId,
      width = width,
      height = 1080,
      fps = 60,
      bitrateKbps = 20_000,
      packetSize = 1024,
      audioConfiguration = audioConfiguration,
      supportedVideoFormats = supportedFormats,
    )
  }

  // --- Review1121 defect 1: saved pin must be installed before use ---------

  @Test
  fun savedPinIsInstalledBeforeServerInfoAndAcceptedByConcreteClient() {
    val fixture = Fixture()
    fixture.trustStore.save(fixture.crypto.clientCertificate)
    fixture.host.info = pairedInfo()

    val result = fixture.session.connect(fixture.host, fixture.plan(), pin = null)
    assertTrue(result is ZenMoonlightSession.Result.Started)
    assertEquals(listOf("pin", "fetch"), fixture.host.events.take(2))
    assertEquals(fixture.crypto.clientCertificate, fixture.host.getServerCert())

    // The concrete HTTP boundary accepts the very certificate loaded from disk.
    val http = MoonlightNvHttp("192.0.2.10", 47989, 47984, "ZEN0000000000001", "zen", null, fixture.crypto)
    http.setServerCert(fixture.trustStore.load())
    http.trustManagerForTesting()
      .checkServerTrusted(arrayOf(fixture.trustStore.load()), "RSA")
  }

  @Test
  fun corruptSavedTrustIsReportedWithoutResettingIdentity() {
    val fixture = Fixture()
    Files.write(fixture.trustStore.file().toPath(), "not a certificate".toByteArray())
    val identityBefore = fixture.crypto.clientCertificate

    val result = fixture.session.connect(fixture.host, fixture.plan(), pin = "1234")
    assertEquals("trust_store_corrupt", (result as ZenMoonlightSession.Result.Rejected).reason)
    assertEquals(0, fixture.host.fetchCalls)
    assertTrue(File(fixture.dir, "client.crt").isFile)
    assertEquals(identityBefore, ZenCryptoProvider(fixture.dir).clientCertificate)
  }

  // --- Review1121 defect 2: revoke/stop fence around in-flight connect -----

  @Test
  fun revokedDuringLaunchNeverStartsCore() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo()
    fixture.host.setServerCert(fixture.crypto.clientCertificate)
    fixture.host.onLaunch = { fixture.session.revoke(fixture.host) }

    val result = fixture.session.connect(fixture.host, fixture.plan(), pin = null)
    assertEquals("revoked", (result as ZenMoonlightSession.Result.Rejected).reason)
    assertTrue(fixture.core.configs.isEmpty())
    assertTrue(fixture.host.cancelCalls >= 1)
    assertNull(fixture.session.inMemoryPin())
    assertNull(fixture.host.getServerCert())
  }

  @Test
  fun stopDuringLaunchNeverStartsCore() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo()
    fixture.host.setServerCert(fixture.crypto.clientCertificate)
    fixture.host.onLaunch = { fixture.session.stop(fixture.host) }

    val result = fixture.session.connect(fixture.host, fixture.plan(), pin = null)
    assertEquals("revoked", (result as ZenMoonlightSession.Result.Rejected).reason)
    assertTrue(fixture.core.configs.isEmpty())
    assertNull(fixture.session.inMemoryPin())
  }

  // --- Review1121 defect 3: launch/resume/foreign-busy policy -------------

  @Test
  fun idleHostLaunches() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo(runningGameId = 0)
    fixture.host.setServerCert(fixture.crypto.clientCertificate)
    val result = fixture.session.connect(fixture.host, fixture.plan(), pin = null)
    result as ZenMoonlightSession.Result.Started
    assertEquals("launch", result.launchVerb)
    assertEquals("launch", fixture.host.lastRequest!!.verb())
  }

  @Test
  fun sameAppResumes() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo(runningGameId = 1)
    fixture.host.setServerCert(fixture.crypto.clientCertificate)
    val result = fixture.session.connect(fixture.host, fixture.plan(appId = 1), pin = null)
    result as ZenMoonlightSession.Result.Started
    assertEquals("resume", result.launchVerb)
    assertEquals("resume", fixture.host.lastRequest!!.verb())
  }

  @Test
  fun foreignRunningAppIsRejectedWithoutQuitting() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo(runningGameId = 2)
    fixture.host.setServerCert(fixture.crypto.clientCertificate)
    val result = fixture.session.connect(fixture.host, fixture.plan(appId = 1), pin = null)
    assertEquals("host_busy_foreign_app", (result as ZenMoonlightSession.Result.Rejected).reason)
    assertNull(fixture.host.lastRequest)
    assertEquals(0, fixture.host.quitCalls)
    assertTrue(fixture.core.configs.isEmpty())
  }

  // --- Review1121 defect 4: masks and header values -----------------------

  @Test
  fun videoFormatConstantsMatchPinnedHeader() {
    assertEquals(0x1000, MoonlightVideoFormats.AV1_MAIN8)
    assertEquals(0x10000, MoonlightVideoFormats.SCM_AV1_MAIN8)
    assertEquals(0x1000, MoonlightCore.VIDEO_FORMAT_AV1_MAIN8)
    assertEquals(0x10000, MoonlightCore.SERVER_CODEC_MODE_AV1_MAIN8)
    assertEquals(0x0001, MoonlightVideoFormats.H264)
    assertEquals(0x0100, MoonlightVideoFormats.H265)
    assertEquals(0x0400, MoonlightVideoFormats.H265_REXT8_444)
    assertEquals(0x2000, MoonlightVideoFormats.AV1_MAIN10)
    assertTrue(MoonlightVideoFormats.isRenderable(MoonlightVideoFormats.H264))
    // The wired renderer decodes H.264 only: HEVC in the same mask is rejected.
    assertFalse(MoonlightVideoFormats.isRenderable(MoonlightVideoFormats.H264 or MoonlightVideoFormats.H265))
    assertFalse(MoonlightVideoFormats.isRenderable(0))
    // Host SCM bit must never be accepted as a client decoder format.
    assertFalse(MoonlightVideoFormats.isRenderable(0x10000))
    assertFalse(MoonlightVideoFormats.isRenderable(MoonlightVideoFormats.H265))
    // Reserved H.264 mask bits are not advertised either.
    assertFalse(MoonlightVideoFormats.isRenderable(0x0002))
    assertFalse(MoonlightVideoFormats.isRenderable(0x0001 or 0x0008))
  }

  @Test
  fun invalidVideoFormatsRejectedBeforeLaunch() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo()
    fixture.host.setServerCert(fixture.crypto.clientCertificate)
    val result = fixture.session.connect(fixture.host, fixture.plan(supportedFormats = 0x10000), pin = null)
    assertEquals("invalid_video_formats", (result as ZenMoonlightSession.Result.Rejected).reason)
    assertNull(fixture.host.lastRequest)
  }

  @Test
  fun invalidAudioAndResolutionRejectedBeforeLaunch() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo()
    fixture.host.setServerCert(fixture.crypto.clientCertificate)
    val badAudio = fixture.session.connect(fixture.host, fixture.plan(audioConfiguration = 2), pin = null)
    assertEquals("invalid_audio_configuration", (badAudio as ZenMoonlightSession.Result.Rejected).reason)
    val badSize = fixture.session.connect(fixture.host, fixture.plan(width = 0), pin = null)
    assertEquals("invalid_resolution", (badSize as ZenMoonlightSession.Result.Rejected).reason)
    assertNull(fixture.host.lastRequest)
  }

  // --- Connected-path contract -------------------------------------------

  @Test
  fun pairingThenLaunchHandsTheSameRiKeyToTheCore() {
    val fixture = Fixture()
    val result = fixture.session.connect(fixture.host, fixture.plan(), pin = "1234")
    result as ZenMoonlightSession.Result.Started
    assertEquals(PairingManager.PairState.PAIRED, result.pairState)
    assertTrue(result.launchAccepted)
    assertTrue(result.startAccepted)
    assertEquals("launch", result.launchVerb)
    assertEquals("rtsp://192.0.2.10:48010", result.rtspSessionUrl)
    assertEquals(fixture.crypto.clientCertificate, fixture.trustStore.load())

    val query = fixture.host.lastRequest!!.toQuery()
    val launchKey = query.substringAfter("&rikey=").substringBefore("&")
    val launchKeyId = query.substringAfter("&rikeyid=").substringBefore("&").toInt()
    val config = fixture.core.configs.single()
    assertEquals(launchKey.uppercase(), config.remoteInputAesKey.joinToString("") { "%02X".format(it) })
    val ivKeyId = ((config.remoteInputAesIv[0].toInt() and 0xFF) shl 24) or
      ((config.remoteInputAesIv[1].toInt() and 0xFF) shl 16) or
      ((config.remoteInputAesIv[2].toInt() and 0xFF) shl 8) or
      (config.remoteInputAesIv[3].toInt() and 0xFF)
    assertEquals(launchKeyId, ivKeyId)
    assertEquals(MoonlightVideoFormats.DECODER_SUPPORTED, config.supportedVideoFormats)
    assertEquals(256, config.serverCodecModeSupport)
  }

  @Test
  fun pairedHostSkipsPairingAndReusesStoredPin() {
    val fixture = Fixture()
    fixture.trustStore.save(fixture.crypto.clientCertificate)
    fixture.host.info = pairedInfo()
    val result = fixture.session.connect(fixture.host, fixture.plan(), pin = null)
    result as ZenMoonlightSession.Result.Started
    assertEquals(0, fixture.host.pairing.pairCalls)
    assertNull(result.pairState)
  }

  @Test
  fun startAcceptanceIsSeparateFromEstablishment() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo()
    fixture.host.setServerCert(fixture.crypto.clientCertificate)
    fixture.core.startResult = -2
    val result = fixture.session.connect(fixture.host, fixture.plan(), pin = null)
    result as ZenMoonlightSession.Result.Started
    assertTrue(result.launchAccepted)
    assertFalse(result.startAccepted)
  }

  @Test
  fun revokeStopsCoreQuitsUnpairsAndClearsTrust() {
    val fixture = Fixture()
    fixture.trustStore.save(fixture.crypto.clientCertificate)
    val report = fixture.session.revoke(fixture.host)
    assertEquals(0, report.stopResult)
    assertTrue(report.complete)
    assertTrue(report.quitSucceeded)
    assertTrue(report.unpairSucceeded)
    assertTrue(report.trustCleared)
    assertEquals(1, fixture.core.stopCalls)
    assertEquals(1, fixture.host.quitCalls)
    assertEquals(1, fixture.host.unpairCalls)
    assertNull(fixture.trustStore.load())
    assertNull(fixture.session.inMemoryPin())
    assertTrue(fixture.host.cancelCalls >= 1)
  }

  @Test
  fun twoThreadStopDuringLaunchPreventsAnyCoreStart() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo()
    fixture.host.setServerCert(fixture.crypto.clientCertificate)
    val launchEntered = java.util.concurrent.CountDownLatch(1)
    val releaseLaunch = java.util.concurrent.CountDownLatch(1)
    fixture.host.onLaunch = {
      launchEntered.countDown()
      releaseLaunch.await()
    }

    val result = java.util.concurrent.atomic.AtomicReference<ZenMoonlightSession.Result>()
    val worker = Thread { result.set(fixture.session.connect(fixture.host, fixture.plan(), pin = null)) }
    worker.start()
    assertTrue(launchEntered.await(2, java.util.concurrent.TimeUnit.SECONDS))

    // stop runs on another thread and must return before the launch does.
    val stopResult = fixture.session.stop(fixture.host)
    assertEquals(0, stopResult)
    releaseLaunch.countDown()
    worker.join(5_000)
    assertEquals("revoked", (result.get() as ZenMoonlightSession.Result.Rejected).reason)
    assertTrue("core must not start after stop returned", fixture.core.configs.isEmpty())
  }

  @Test
  fun revokeKeepsScopedPinInstalledForAuthenticatedQuit() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo()
    fixture.host.setServerCert(fixture.crypto.clientCertificate)
    val report = fixture.session.revoke(fixture.host)
    assertTrue(report.complete)
    assertNotNull("quit must run with the pinned certificate still installed", fixture.host.pinAtQuit)
    assertEquals(fixture.crypto.clientCertificate, fixture.host.pinAtQuit)
    assertNull(fixture.host.getServerCert())
  }

  @Test
  fun revokeIsNotCompleteWhenStopOrQuitFail() {
    val fixture = Fixture()
    fixture.trustStore.save(fixture.crypto.clientCertificate)
    fixture.host.quitFailure = true
    fixture.core.stopResult = -3
    val report = fixture.session.revoke(fixture.host)
    assertFalse(report.complete)
    assertEquals(-3, report.stopResult)
    assertFalse(report.quitSucceeded)
    assertTrue(report.failures.any { it.startsWith("stop:") })
    assertTrue(report.failures.any { it.startsWith("quit") })
  }

  @Test
  fun revokeReportSurfacesHostFailures() {
    val fixture = Fixture()
    fixture.trustStore.save(fixture.crypto.clientCertificate)
    fixture.host.quitFailure = true
    fixture.host.unpairFailure = true
    val report = fixture.session.revoke(fixture.host)
    assertFalse(report.quitSucceeded)
    assertFalse(report.unpairSucceeded)
    assertTrue(report.trustCleared)
    assertFalse(report.complete)
    assertTrue(report.failures.any { it.startsWith("quit") })
    assertTrue(report.failures.any { it.startsWith("unpair") })
  }
}

private fun unpairedInfo(runningGameId: Int = 0): MoonlightServerInfo =
  serverInfo(paired = false, codecSupport = 256, runningGameId = runningGameId)

private fun pairedInfo(codecSupport: Int = 256, runningGameId: Int = 0): MoonlightServerInfo =
  serverInfo(paired = true, codecSupport = codecSupport, runningGameId = runningGameId)

private fun serverInfo(paired: Boolean, codecSupport: Int, runningGameId: Int): MoonlightServerInfo =
  MoonlightServerInfo.parse(
    "<root status_code=\"200\"><appversion>7.1.431.0</appversion>" +
      "<GfeVersion>Sunshine/0.23.1</GfeVersion><ServerCodecModeSupport>$codecSupport</ServerCodecModeSupport>" +
      "<PairStatus>${if (paired) 1 else 0}</PairStatus><currentgame>$runningGameId</currentgame></root>",
  )
