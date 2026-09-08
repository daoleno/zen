package expo.modules.zenremotedesktop

import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import org.junit.Assert.*
import org.junit.Test

class DecodeAdmissionTest {
  @Test fun codecWaitDrainsAndRetriesTemporaryBufferUnavailability() {
    var attempts = 0
    var drains = 0
    val index = awaitDecoderInput({ true }, { timeout ->
      assertEquals(10000L, timeout)
      if (++attempts < 5) -1 else 3
    }, { drains++ }, { attempts * 10000000L })
    assertEquals(3, index)
    assertEquals(5, drains)
  }

  @Test fun codecWaitTerminatesAtDeadline() {
    var time = 0L
    try {
      awaitDecoderInput({ true }, { time += 10000000L; -1 }, {}, { time })
      fail("Expected bounded codec wait")
    } catch (error: IllegalStateException) {
      assertEquals("decoder_backpressure", error.message)
      assertEquals(2000000000L, time)
    }
  }

  @Test fun codecWaitRejectsBufferFromRetiredGeneration() {
    var current = true
    try {
      awaitDecoderInput({ current }, { current = false; 2 }, {})
      fail("Expected stale buffer rejection")
    } catch (error: IllegalStateException) {
      assertEquals("decoder_generation_ended", error.message)
    }
  }

  @Test fun startupWaitsForAReleasedSlotWithoutIncreasingCapacity() {
    val admission = DecodeAdmission()
    assertTrue(admission.acquire({ true }))
    assertTrue(admission.acquire({ true }))
    val executor = Executors.newSingleThreadExecutor()
    val started = CountDownLatch(1)
    try {
      val waiting = executor.submit<Boolean> {
        started.countDown()
        admission.acquire({ true })
      }
      assertTrue(started.await(1, TimeUnit.SECONDS))
      assertFalse(waiting.isDone)
      admission.release()
      assertTrue(waiting.get(1, TimeUnit.SECONDS))
      assertFalse(admission.acquire({ true }, 10))
      admission.release()
      admission.release()
      assertTrue(admission.acquire({ true }, 10))
      assertTrue(admission.acquire({ true }, 10))
    } finally { executor.shutdownNow() }
  }

  @Test fun staleWaiterReturnsItsSlotAndCannotEnterNewGeneration() {
    val admission = DecodeAdmission()
    repeat(2) { assertTrue(admission.acquire({ true })) }
    val current = AtomicBoolean(true)
    val checked = CountDownLatch(1)
    val executor = Executors.newSingleThreadExecutor()
    try {
      val waiting = executor.submit<Boolean> {
        admission.acquire({ current.get().also { checked.countDown() } })
      }
      assertTrue(checked.await(1, TimeUnit.SECONDS))
      current.set(false)
      admission.release()
      assertFalse(waiting.get(1, TimeUnit.SECONDS))
      assertTrue(admission.acquire({ true }, 10))
      assertFalse(admission.acquire({ true }, 10))
    } finally { executor.shutdownNow() }
  }

  @Test fun expiredWaitDoesNotReleaseAnOccupiedSlot() {
    val admission = DecodeAdmission()
    repeat(2) { assertTrue(admission.acquire({ true })) }
    repeat(2) { assertFalse(admission.acquire({ true }, 10)) }
    admission.release()
    assertFalse(admission.acquire({ false }, 10))
    assertTrue(admission.acquire({ true }, 10))
    assertFalse(admission.acquire({ true }, 10))
  }
}
