package expo.modules.zenremotedesktop

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Before
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

/**
 * Executable bridge lifecycle tests against the inert moonlignt-common-c
 * boundary in libzen_moonlight_test.so. No network, host or real stream is
 * involved: the fake core records calls and lets the test drive lifecycle
 * events exactly like the pinned core does (start returns with a live session,
 * termination arrives later, stop must call LiStopConnection once).
 *
 * Run with: ZEN_MOONLIGHT_BRIDGE_TEST=1 ./gradlew :zen-remote-desktop:testDebugUnitTest
 */
class MoonlightCoreBridgeTest {

  companion object {
    init {
      val path = System.getProperty("zen.moonlight.bridge.test.lib")
      if (path != null && java.io.File(path).isFile) {
        System.load(path)
      }
    }
  }

  private fun awaitState(core: MoonlightCore, expected: Int, timeoutMs: Long = 5_000): Int {
    val deadline = System.currentTimeMillis() + timeoutMs
    while (System.currentTimeMillis() < deadline) {
      if (core.sessionState() and 0x0F == expected) return core.sessionState()
      Thread.sleep(10)
    }
    return core.sessionState()
  }

  private fun config(key: ByteArray = MoonlightKeyMaterial.generateKey(), iv: ByteArray = MoonlightKeyMaterial.generateIv()): MoonlightCore.Config =
    MoonlightCore.Config(
      address = "192.0.2.10",
      appVersion = "7.1.431.0",
      gfeVersion = "",
      rtspSessionUrl = "",
      serverCodecModeSupport = MoonlightCore.SERVER_CODEC_MODE_H264,
      width = 1920,
      height = 1080,
      fps = 60,
      bitrateKbps = 20_000,
      packetSize = 1024,
      audioConfiguration = MoonlightAudioConfiguration.STEREO,
      supportedVideoFormats = MoonlightCore.VIDEO_FORMAT_H264,
      clientRefreshRateX100 = 0,
      streamingRemotely = 0,
      remoteInputAesKey = key,
      remoteInputAesIv = iv,
    )

  private class TestCore : MoonlightCore() {
    val started = CountDownLatch(1)
    val terminated = CountDownLatch(1)
    val decoded = CountDownLatch(1)
    val stages = AtomicInteger()
    var failDecode = false
    var lastTerminationCode = 0

    override fun onConnectionStarted() {
      started.countDown()
    }

    override fun onConnectionStage(stage: Int) {
      stages.incrementAndGet()
    }

    override fun onConnectionTerminated(errorCode: Int) {
      lastTerminationCode = errorCode
      terminated.countDown()
    }

    override fun onDecodeUnit(data: ByteArray, frameType: Int, presentationTimeUs: Long): Int {
      if (failDecode) throw IllegalStateException("decoder refused the frame")
      decoded.countDown()
      return 0
    }
  }

  @Before
  fun requireBridgeTestLibrary() {
    assumeTrue(
      "host bridge test library not configured (build it with scripts/build-moonlight-bridge-test.sh)",
      System.getProperty("zen.moonlight.bridge.test.lib") != null &&
        java.io.File(System.getProperty("zen.moonlight.bridge.test.lib")).isFile,
    )
    MoonlightTestProbe.reset()
  }

  @Test
  fun startReturnsWithLiveSessionAndLateCallbacksStillArrive() {
    val core = TestCore()
    assertEquals(0, core.start(config()))
    assertTrue(core.started.await(2, TimeUnit.SECONDS))
    assertEquals(MoonlightCore.SESSION_ACTIVE, awaitState(core, MoonlightCore.SESSION_ACTIVE) and 0x0F)

    // The fake core only invoked the start-time stage callback; a post-start
    // decode unit must still reach the listener.
    MoonlightTestProbe.emitDecodeUnit(byteArrayOf(1, 2, 3, 4), MoonlightCore.FRAME_TYPE_IDR, 42, 4)
    assertTrue(core.decoded.await(2, TimeUnit.SECONDS))
    assertEquals(MoonlightCore.SESSION_ACTIVE, core.sessionState() and 0x0F)

    MoonlightTestProbe.emitTerminated(7)
    assertTrue(core.terminated.await(2, TimeUnit.SECONDS))
    assertEquals(7, core.lastTerminationCode)
    assertEquals(MoonlightCore.SESSION_IDLE, awaitState(core, MoonlightCore.SESSION_IDLE) and 0x0F)
    assertEquals(1, MoonlightTestProbe.stopCount())
  }

  @Test
  fun stopCallsRealBoundaryOnceAndOnlyTheOwnerMayStop() {
    val owner = TestCore()
    assertEquals(0, owner.start(config()))
    assertTrue(owner.started.await(2, TimeUnit.SECONDS))

    val other = MoonlightCore()
    assertEquals(-3, other.stop())
    assertEquals(0, MoonlightTestProbe.stopCount())

    assertEquals(0, owner.stop())
    assertEquals(MoonlightCore.SESSION_IDLE, awaitState(owner, MoonlightCore.SESSION_IDLE) and 0x0F)
    assertEquals(1, MoonlightTestProbe.stopCount())
  }

  @Test
  fun overlappingStartIsRejectedWhileSessionIsLive() {
    val core = TestCore()
    assertEquals(0, core.start(config()))
    assertTrue(core.started.await(2, TimeUnit.SECONDS))
    assertEquals(-2, core.start(config()))
    assertEquals(1, MoonlightTestProbe.startCount())
    assertEquals(0, core.stop())
  }

  @Test
  fun stopWhileStartingInterruptsAndRetiresWithoutSecondStop() {
    MoonlightTestProbe.setStartBlocks(true)
    val core = TestCore()
    assertEquals(0, core.start(config()))

    val deadline = System.currentTimeMillis() + 5_000
    while (!MoonlightTestProbe.startEntered() && System.currentTimeMillis() < deadline) {
      Thread.sleep(10)
    }
    assertTrue(MoonlightTestProbe.startEntered())

    assertEquals(0, core.stop())
    assertTrue(MoonlightTestProbe.interruptCount() >= 1)
    assertEquals(0, MoonlightTestProbe.stopCount())
    assertEquals(MoonlightCore.SESSION_IDLE, core.sessionState() and 0x0F)

    // The failed start must not leave the session locked for the next start.
    MoonlightTestProbe.setStartBlocks(false)
    assertEquals(0, core.start(config()))
    assertTrue(core.started.await(2, TimeUnit.SECONDS))
    assertEquals(0, core.stop())
    assertEquals(1, MoonlightTestProbe.stopCount())
  }

  @Test
  fun missingOrShortKeyMaterialIsRejectedBeforeAnyStart() {
    val core = TestCore()
    assertEquals(-4, core.start(config(key = ByteArray(15), iv = MoonlightKeyMaterial.generateIv())))
    assertEquals(-4, core.start(config(key = MoonlightKeyMaterial.generateKey(), iv = ByteArray(15))))
    assertEquals(-4, core.start(config(key = ByteArray(16), iv = MoonlightKeyMaterial.generateIv())))
    assertEquals(-4, core.start(config(key = MoonlightKeyMaterial.generateKey(), iv = ByteArray(0))))
    assertEquals(0, MoonlightTestProbe.startCount())
  }

  @Test
  fun packedAudioConfigurationMatchesUpstreamLayout() {
    assertEquals(0x302CA, MoonlightAudioConfiguration.STEREO)
    assertEquals(0x3F06CA, MoonlightAudioConfiguration.SURROUND_51)
    assertEquals(0x63F08CA, MoonlightAudioConfiguration.SURROUND_71)
    assertEquals(2, MoonlightAudioConfiguration.channelCount(MoonlightAudioConfiguration.STEREO))
    assertEquals(0x3, MoonlightAudioConfiguration.channelMask(MoonlightAudioConfiguration.STEREO))
    assertTrue(MoonlightAudioConfiguration.isValid(MoonlightAudioConfiguration.SURROUND_71))
    assertFalse(MoonlightAudioConfiguration.isValid(2))
    assertFalse(MoonlightAudioConfiguration.isValid(0))
  }

  @Test
  fun invalidStreamArgumentsAreRejectedBeforeAnyStart() {
    val core = TestCore()
    assertEquals(-5, core.start(config().copy(width = 0)))
    assertEquals(-5, core.start(config().copy(packetSize = 8)))
    assertEquals(-5, core.start(config().copy(audioConfiguration = 2)))
    assertEquals(-5, core.start(config().copy(supportedVideoFormats = 0)))
    assertEquals(0, MoonlightTestProbe.startCount())
  }

  @Test
  fun utf8TextIsForwardedAsStandardUtf8Bytes() {
    val core = TestCore()
    assertEquals(0, core.start(config()))
    assertTrue(core.started.await(2, TimeUnit.SECONDS))

    val text = "汉字😀"
    assertEquals(0, core.sendUtf8TextEvent(text))
    assertArrayEquals(text.toByteArray(Charsets.UTF_8), MoonlightTestProbe.lastUtf8Bytes())

    assertEquals(-5, core.sendUtf8TextEvent(""))
    assertEquals(0, core.sendKeyboardEvent(65, MoonlightCore.KEY_ACTION_DOWN, 0))
    assertEquals(65, MoonlightTestProbe.lastKeyCode().toInt())

    val foreign = MoonlightCore()
    assertEquals(-3, foreign.sendUtf8TextEvent("x"))
    assertEquals(-3, foreign.sendKeyboardEvent(66, MoonlightCore.KEY_ACTION_DOWN, 0))

    assertEquals(0, core.stop())
  }

  @Test
  fun callbackFailureRetiresTheSessionSafely() {
    val core = TestCore()
    core.failDecode = true
    assertEquals(0, core.start(config()))
    assertTrue(core.started.await(2, TimeUnit.SECONDS))
    assertEquals(MoonlightCore.SESSION_ACTIVE, core.sessionState() and 0x0F)

    MoonlightTestProbe.emitDecodeUnit(byteArrayOf(9, 9, 9), MoonlightCore.FRAME_TYPE_PFRAME, 1, 3)

    assertEquals(MoonlightCore.SESSION_IDLE, awaitState(core, MoonlightCore.SESSION_IDLE) and 0x0F)
    assertTrue(core.sessionState() and MoonlightCore.STATE_FLAG_CALLBACK_FAILED != 0)
    assertEquals(1, MoonlightTestProbe.stopCount())
  }

  @Test
  fun malformedDecodeUnitIsRejectedWithoutEnteringJava() {
    val core = TestCore()
    assertEquals(0, core.start(config()))
    assertTrue(core.started.await(2, TimeUnit.SECONDS))

    // Declared length is smaller than the buffer list; the bridge must drop it.
    MoonlightTestProbe.emitDecodeUnit(byteArrayOf(1, 2, 3, 4, 5), MoonlightCore.FRAME_TYPE_IDR, 1, 2)
    assertFalse(core.decoded.await(250, TimeUnit.MILLISECONDS))
    assertEquals(MoonlightCore.SESSION_ACTIVE, core.sessionState() and 0x0F)

    assertEquals(0, core.stop())
  }

  private fun awaitCondition(timeoutMs: Long = 5_000, condition: () -> Boolean) {
    val deadline = System.currentTimeMillis() + timeoutMs
    while (System.currentTimeMillis() < deadline) {
      if (condition()) return
      Thread.sleep(10)
    }
    assertTrue("condition not met within ${timeoutMs}ms", condition())
  }

  @Test
  fun stopFromStartedCallbackDefersInsteadOfDeadlocking() {
    var stopResult = -999
    val entered = CountDownLatch(1)
    val core = object : MoonlightCore() {
      override fun onConnectionStarted() {
        entered.countDown()
        stopResult = stop()
      }
    }
    assertEquals(0, core.start(config()))
    assertTrue(entered.await(2, TimeUnit.SECONDS))
    awaitCondition { stopResult != -999 }
    assertEquals(0, stopResult)
    assertEquals(MoonlightCore.SESSION_IDLE, awaitState(core, MoonlightCore.SESSION_IDLE) and 0x0F)
    assertEquals(1, MoonlightTestProbe.stopCount())
  }

  @Test
  fun stopFromStageAndSetupCallbacksDefersWithoutDeadlock() {
    val stageEntered = CountDownLatch(1)
    val stageStop = object : MoonlightCore() {
      override fun onConnectionStage(stage: Int) {
        stageEntered.countDown()
        stop()
      }
    }
    assertEquals(0, stageStop.start(config()))
    assertTrue(stageEntered.await(2, TimeUnit.SECONDS))
    assertEquals(MoonlightCore.SESSION_IDLE, awaitState(stageStop, MoonlightCore.SESSION_IDLE) and 0x0F)
    assertTrue(MoonlightTestProbe.stopCount() <= 1)
    // No stale stop request may leak into the next start.
    MoonlightTestProbe.reset()
    val next = TestCore()
    assertEquals(0, next.start(config()))
    assertTrue(next.started.await(2, TimeUnit.SECONDS))
    assertEquals(MoonlightCore.SESSION_ACTIVE, next.sessionState() and 0x0F)
    assertEquals(0, next.stop())

    MoonlightTestProbe.reset()
    val setupEntered = CountDownLatch(1)
    val setupStop = object : MoonlightCore() {
      override fun onVideoSetup(videoFormat: Int, width: Int, height: Int, redrawRate: Int): Int {
        setupEntered.countDown()
        stop()
        return 0
      }
    }
    assertEquals(0, setupStop.start(config()))
    assertTrue(setupEntered.await(2, TimeUnit.SECONDS))
    assertEquals(MoonlightCore.SESSION_IDLE, awaitState(setupStop, MoonlightCore.SESSION_IDLE) and 0x0F)
    assertTrue(MoonlightTestProbe.stopCount() <= 1)
  }

  @Test
  fun setupExceptionSurfacesAsyncStartFailureAndRetires() {
    val failed = java.util.concurrent.atomic.AtomicInteger(0)
    val failedLatch = CountDownLatch(1)
    val core = object : MoonlightCore() {
      override fun onVideoSetup(videoFormat: Int, width: Int, height: Int, redrawRate: Int): Int =
        throw IllegalStateException("decoder refused the stream")

      override fun onConnectionStartFailed(errorCode: Int) {
        failed.set(errorCode)
        failedLatch.countDown()
      }
    }
    assertEquals(0, core.start(config()))
    assertTrue(failedLatch.await(2, TimeUnit.SECONDS))
    assertEquals(-1, failed.get())
    assertEquals(-1, core.lastStartError())
    assertEquals(MoonlightCore.SESSION_IDLE, awaitState(core, MoonlightCore.SESSION_IDLE) and 0x0F)
    assertTrue(core.sessionState() and MoonlightCore.STATE_FLAG_CALLBACK_FAILED != 0)

    // The failed start must retire fully so a new session can start.
    MoonlightTestProbe.reset()
    val next = TestCore()
    assertEquals(0, next.start(config()))
    assertTrue(next.started.await(2, TimeUnit.SECONDS))
    assertEquals(0, next.stop())
  }

  @Test
  fun staleTeardownFromPreviousSessionCannotStopNewSession() {
    MoonlightTestProbe.setTeardownDelayMs(400)
    try {
      val first = TestCore()
      assertEquals(0, first.start(config()))
      assertTrue(first.started.await(2, TimeUnit.SECONDS))
      MoonlightTestProbe.emitTerminated(0)
      Thread.sleep(80) // teardown request is enqueued and sleeping

      assertEquals(0, first.stop())
      assertEquals(MoonlightCore.SESSION_IDLE, awaitState(first, MoonlightCore.SESSION_IDLE) and 0x0F)
      assertEquals(1, MoonlightTestProbe.stopCount())

      val second = TestCore()
      assertEquals(0, second.start(config()))
      assertTrue(second.started.await(2, TimeUnit.SECONDS))

      // The delayed teardown from the first session must not stop the second.
      Thread.sleep(600)
      assertEquals(MoonlightCore.SESSION_ACTIVE, second.sessionState() and 0x0F)
      assertEquals(1, MoonlightTestProbe.stopCount())

      // The retired owner may no longer stop the live session.
      assertEquals(-3, first.stop())
      assertEquals(1, MoonlightTestProbe.stopCount())
      assertEquals(0, second.stop())
      assertEquals(2, MoonlightTestProbe.stopCount())
    } finally {
      MoonlightTestProbe.setTeardownDelayMs(0)
    }
  }

  @Test
  fun newSessionKeepsItsOwnListenerReference() {
    val first = TestCore()
    assertEquals(0, first.start(config()))
    assertTrue(first.started.await(2, TimeUnit.SECONDS))
    assertEquals(0, first.stop())
    assertEquals(MoonlightCore.SESSION_IDLE, awaitState(first, MoonlightCore.SESSION_IDLE) and 0x0F)

    val second = TestCore()
    assertEquals(0, second.start(config()))
    assertTrue(second.started.await(2, TimeUnit.SECONDS))
    MoonlightTestProbe.emitDecodeUnit(byteArrayOf(7, 7, 7, 7), MoonlightCore.FRAME_TYPE_IDR, 5, 4)
    assertTrue(second.decoded.await(2, TimeUnit.SECONDS))
    assertEquals(MoonlightCore.SESSION_ACTIVE, second.sessionState() and 0x0F)
    assertEquals(0, second.stop())
  }
}
