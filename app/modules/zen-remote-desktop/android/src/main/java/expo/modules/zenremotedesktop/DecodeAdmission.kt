package expo.modules.zenremotedesktop

import java.util.concurrent.Semaphore
import java.util.concurrent.TimeUnit

internal class DecodeAdmission {
  private val slots = Semaphore(2)

  fun acquire(isCurrent: () -> Boolean, timeoutMillis: Long = 2000): Boolean {
    if (!isCurrent()) return false
    val acquired = try {
      slots.tryAcquire(timeoutMillis, TimeUnit.MILLISECONDS)
    } catch (_: InterruptedException) {
      Thread.currentThread().interrupt()
      false
    }
    if (!acquired) return false
    if (isCurrent()) return true
    slots.release()
    return false
  }

  fun release() = slots.release()
}

internal fun awaitDecoderInput(
  isCurrent: () -> Boolean,
  dequeue: (Long) -> Int,
  drain: () -> Unit,
  nowNanos: () -> Long = System::nanoTime,
): Int {
  val deadline = nowNanos() + TimeUnit.SECONDS.toNanos(2)
  while (isCurrent()) {
    drain()
    val index = dequeue(10000)
    if (!isCurrent()) break
    if (index >= 0) return index
    if (nowNanos() >= deadline) throw IllegalStateException("decoder_backpressure")
  }
  throw IllegalStateException("decoder_generation_ended")
}
