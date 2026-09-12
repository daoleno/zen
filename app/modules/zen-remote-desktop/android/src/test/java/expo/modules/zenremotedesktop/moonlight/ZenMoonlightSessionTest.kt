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
import java.nio.file.Files
import java.security.cert.X509Certificate

/**
 * Executable orchestration regression for the one connected path: upstream
 * pairing/serverinfo/launch must hand real negotiated material to the C core,
 * and start acceptance must stay separate from stream establishment.
 */
class ZenMoonlightSessionTest {

  private class RecordingCore : ZenMoonlightSession.CoreControl {
    val configs = mutableListOf<MoonlightCore.Config>()
    var startResult = 0
    var stopCalls = 0

    override fun start(config: MoonlightCore.Config): Int {
      configs += config
      return startResult
    }

    override fun stop(): Int {
      stopCalls++
      return 0
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

    override fun getServerCert(): X509Certificate? = pinnedCert

    override fun setServerCert(serverCert: X509Certificate?) {
      pinnedCert = serverCert
    }

    var expectedPinnedCert: X509Certificate? = null
    var launchOutcome = MoonlightHost.LaunchOutcome(true, "rtsp://192.0.2.10:48010")
    var lastRequest: MoonlightLaunchRequest? = null
    var quitCalls = 0
    var unpairCalls = 0
    lateinit var pairing: StubPairingManager

    override fun address(): String = "192.0.2.10"
    override fun fetchServerInfo(): MoonlightServerInfo = info
    override fun getPairingManager(): PairingManager = pairing

    override fun launchOrResume(request: MoonlightLaunchRequest): MoonlightHost.LaunchOutcome {
      lastRequest = request
      return launchOutcome
    }

    override fun quitApp(): Boolean {
      quitCalls++
      return true
    }

    override fun unpair() {
      unpairCalls++
    }
  }

  private class Fixture {
    val identityDir: File = Files.createTempDirectory("zen-session-test").toFile()
    val crypto = ZenCryptoProvider(identityDir)
    val trustStore = ZenHostTrustStore(identityDir)
    val core = RecordingCore()
    val serverCert: X509Certificate get() = crypto.clientCertificate
    lateinit var host: FakeHost

    init {
      host = FakeHost(unpairedInfo())
      host.expectedPinnedCert = serverCert
      host.pairing = StubPairingManager(crypto, host)
    }

    fun session() = ZenMoonlightSession(trustStore, core)

    fun plan(supportedFormats: Int = 0x0001) = ZenMoonlightSession.Plan(
      appId = 1,
      width = 1920,
      height = 1080,
      fps = 60,
      bitrateKbps = 20_000,
      packetSize = 1024,
      audioConfiguration = MoonlightAudioConfiguration.STEREO,
      supportedVideoFormats = supportedFormats,
    )
  }

  @Test
  fun unpairedHostWithoutPinIsRejectedBeforeAnyLaunch() {
    val fixture = Fixture()
    val result = fixture.session().connect(fixture.host, fixture.plan(), pin = null)
    assertTrue(result is ZenMoonlightSession.Result.Rejected)
    assertEquals("pairing_required", (result as ZenMoonlightSession.Result.Rejected).reason)
    assertTrue(fixture.core.configs.isEmpty())
    assertNull(fixture.host.lastRequest)
  }

  @Test
  fun pairingThenLaunchHandsTheSameRiKeyToTheCore() {
    val fixture = Fixture()
    val result = fixture.session().connect(fixture.host, fixture.plan(), pin = "1234")
    assertTrue(result is ZenMoonlightSession.Result.Started)
    result as ZenMoonlightSession.Result.Started
    assertEquals(PairingManager.PairState.PAIRED, result.pairState)
    assertTrue(result.launchAccepted)
    assertTrue(result.startAccepted)
    assertEquals("rtsp://192.0.2.10:48010", result.rtspSessionUrl)

    // The pinned certificate is persisted for the next connection.
    assertEquals(fixture.serverCert, fixture.trustStore.load())

    val query = fixture.host.lastRequest!!.toQuery()
    val launchKey = query.substringAfter("&rikey=").substringBefore("&")
    val launchKeyId = query.substringAfter("&rikeyid=").substringBefore("&").toInt()
    assertEquals(32, launchKey.length)

    val config = fixture.core.configs.single()
    assertEquals("192.0.2.10", config.address)
    assertEquals("rtsp://192.0.2.10:48010", config.rtspSessionUrl)
    assertEquals(
      launchKey.uppercase(),
      config.remoteInputAesKey.joinToString("") { "%02X".format(it) },
    )
    assertEquals(16, config.remoteInputAesIv.size)
    val ivKeyId = ((config.remoteInputAesIv[0].toInt() and 0xFF) shl 24) or
      ((config.remoteInputAesIv[1].toInt() and 0xFF) shl 16) or
      ((config.remoteInputAesIv[2].toInt() and 0xFF) shl 8) or
      (config.remoteInputAesIv[3].toInt() and 0xFF)
    assertEquals(launchKeyId, ivKeyId)
    assertEquals(MoonlightAudioConfiguration.STEREO, config.audioConfiguration)
    assertTrue(config.remoteInputAesKey.any { it != 0.toByte() })
  }

  @Test
  fun pairedHostSkipsPairingAndReusesStoredPin() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo()
    fixture.host.setServerCert(fixture.serverCert)
    val result = fixture.session().connect(fixture.host, fixture.plan(), pin = null)
    assertTrue(result is ZenMoonlightSession.Result.Started)
    assertEquals(0, fixture.host.pairing.pairCalls)
    assertNull((result as ZenMoonlightSession.Result.Started).pairState)
  }

  @Test
  fun pinnedCertificateIsLoadedFromTheTrustStoreWhenHostHasNone() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo()
    fixture.trustStore.save(fixture.serverCert)
    val result = fixture.session().connect(fixture.host, fixture.plan(), pin = null)
    assertTrue(result is ZenMoonlightSession.Result.Started)
  }

  @Test
  fun noCommonVideoFormatIsRejectedBeforeLaunch() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo(codecSupport = 0x0001)
    fixture.host.setServerCert(fixture.serverCert)
    val result = fixture.session().connect(fixture.host, fixture.plan(supportedFormats = 0x0100), pin = null)
    assertEquals("no_common_video_format", (result as ZenMoonlightSession.Result.Rejected).reason)
    assertNull(fixture.host.lastRequest)
  }

  @Test
  fun launchRejectionDoesNotStartTheCore() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo()
    fixture.host.setServerCert(fixture.serverCert)
    fixture.host.launchOutcome = MoonlightHost.LaunchOutcome(false, null)
    val result = fixture.session().connect(fixture.host, fixture.plan(), pin = null)
    assertEquals("launch_rejected", (result as ZenMoonlightSession.Result.Rejected).reason)
    assertTrue(fixture.core.configs.isEmpty())
  }

  @Test
  fun startAcceptanceIsSeparateFromEstablishment() {
    val fixture = Fixture()
    fixture.host.info = pairedInfo()
    fixture.host.setServerCert(fixture.serverCert)
    fixture.core.startResult = -2
    val result = fixture.session().connect(fixture.host, fixture.plan(), pin = null)
    result as ZenMoonlightSession.Result.Started
    assertTrue(result.launchAccepted)
    assertFalse(result.startAccepted)
  }

  @Test
  fun revokeStopsCoreQuitsUnpairsAndClearsTrust() {
    val fixture = Fixture()
    fixture.trustStore.save(fixture.serverCert)
    val stopResult = fixture.session().revoke(fixture.host)
    assertEquals(0, stopResult)
    assertEquals(1, fixture.core.stopCalls)
    assertEquals(1, fixture.host.quitCalls)
    assertEquals(1, fixture.host.unpairCalls)
    assertNull(fixture.trustStore.load())
  }
}

private fun unpairedInfo(): MoonlightServerInfo = serverInfo(paired = false, codecSupport = 0x0001)

private fun pairedInfo(codecSupport: Int = 0x0001): MoonlightServerInfo = serverInfo(paired = true, codecSupport = codecSupport)

private fun serverInfo(paired: Boolean, codecSupport: Int): MoonlightServerInfo =
  MoonlightServerInfo.parse(
    "<root status_code=\"200\"><appversion>7.1.431.0</appversion>" +
      "<GfeVersion>Sunshine/0.23.1</GfeVersion><ServerCodecModeSupport>$codecSupport</ServerCodecModeSupport>" +
      "<PairStatus>${if (paired) 1 else 0}</PairStatus></root>",
  )
