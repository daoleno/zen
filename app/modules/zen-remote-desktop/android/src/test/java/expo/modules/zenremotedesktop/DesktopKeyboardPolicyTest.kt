package expo.modules.zenremotedesktop

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Exact remote-output decisions for the commit-aware keyboard. These run as
 * plain JVM tests: no Android framework, no time windows.
 */
class DesktopKeyboardPolicyTest {
  private val actionDown = 0
  private val actionUp = 1

  @Test
  fun committedTextDispatchesPlainAndPastedTextOnce() {
    assertEquals("abc", DesktopKeyboardPolicy.committedText("abc", -1, -1, false))
    assertEquals("paste-block-42", DesktopKeyboardPolicy.committedText("paste-block-42", -1, -1, false))
  }

  @Test
  fun committedTextIgnoresComposingEmptyAndQuietEdits() {
    assertNull(DesktopKeyboardPolicy.committedText("ni", 0, 2, false))
    assertNull(DesktopKeyboardPolicy.committedText("", -1, -1, false))
    assertNull(DesktopKeyboardPolicy.committedText(null, -1, -1, false))
    assertNull(DesktopKeyboardPolicy.committedText("abc", -1, -1, true))
  }

  @Test
  fun finishedComposingDispatchesThePreeditExactlyOnce() {
    assertEquals("ni", DesktopKeyboardPolicy.finishedComposingText("ni", 0, 2))
    assertEquals("hao", DesktopKeyboardPolicy.finishedComposingText("nihao", 2, 5))
    assertNull(DesktopKeyboardPolicy.finishedComposingText("abc", -1, -1))
    assertNull(DesktopKeyboardPolicy.finishedComposingText("", 0, 0))
    assertNull(DesktopKeyboardPolicy.finishedComposingText("a", 0, 2))
  }

  @Test
  fun deleteSurroundingTextOnAnEmptyBufferSendsTheRequestedCount() {
    assertEquals(listOf("Backspace"), DesktopKeyboardPolicy.deleteKeys(1, 0, 0, -1, -1, 0))
    assertEquals(listOf("Delete"), DesktopKeyboardPolicy.deleteKeys(0, 1, 0, -1, -1, 0))
    assertEquals(3, DesktopKeyboardPolicy.deleteKeys(3, 0, 0, -1, -1, 0).count { it == "Backspace" })
    assertEquals(listOf("Backspace", "Backspace", "Delete"),
      DesktopKeyboardPolicy.deleteKeys(2, 1, 0, -1, -1, 0))
  }

  @Test
  fun deleteSurroundingTextOfComposingPreeditStaysLocal() {
    // "ni" composing 0..2, cursor after it: deleting the preedit sends nothing.
    assertEquals(emptyList<String>(), DesktopKeyboardPolicy.deleteKeys(1, 0, 2, 0, 2, 2))
    assertEquals(emptyList<String>(), DesktopKeyboardPolicy.deleteKeys(2, 0, 2, 0, 2, 2))
    assertEquals(emptyList<String>(), DesktopKeyboardPolicy.deleteKeys(0, 2, 0, 0, 2, 2))
  }

  @Test
  fun deleteSurroundingTextSplitsCommittedAndComposingRuns() {
    // committed "ab" + composing "ni" (length 4, composing 2..4), cursor at 4.
    assertEquals(listOf("Backspace"), DesktopKeyboardPolicy.deleteKeys(3, 0, 4, 2, 4, 4))
    assertEquals(listOf("Backspace", "Backspace"), DesktopKeyboardPolicy.deleteKeys(4, 0, 4, 2, 4, 4))
    // forward delete through the committing boundary
    assertEquals(listOf("Delete"), DesktopKeyboardPolicy.deleteKeys(0, 3, 2, 2, 4, 4))
    assertEquals(emptyList<String>(), DesktopKeyboardPolicy.deleteKeys(0, 2, 2, 2, 4, 4))
  }

  @Test
  fun deleteSurroundingTextPreservesRequestedCountAndClampsComposingRange() {
    // The IME count is the user's intent for the remote buffer, so it is kept
    // even when the local capture buffer is shorter; only composing overlap
    // subtracts from it.
    assertEquals(5, DesktopKeyboardPolicy.deleteKeys(5, 0, 3, -1, -1, 3).count { it == "Backspace" })
    assertEquals(5, DesktopKeyboardPolicy.deleteKeys(0, 5, 3, -1, -1, 3).count { it == "Delete" })
    assertEquals(emptyList<String>(), DesktopKeyboardPolicy.deleteKeys(0, 0, 0, -1, -1, 0))
  }

  @Test
  fun hardwareNamedKeysMapOnceAndConsumeTheUp() {
    assertEquals(DesktopKeyboardPolicy.HardwareAction.NamedKey("Backspace"),
      DesktopKeyboardPolicy.hardwareAction(DesktopKeyboardPolicy.KEYCODE_DEL, 0, false, false, actionDown))
    assertEquals(DesktopKeyboardPolicy.HardwareAction.ConsumeUp,
      DesktopKeyboardPolicy.hardwareAction(DesktopKeyboardPolicy.KEYCODE_DEL, 0, false, false, actionUp))
    assertEquals(DesktopKeyboardPolicy.HardwareAction.NamedKey("Delete"),
      DesktopKeyboardPolicy.hardwareAction(DesktopKeyboardPolicy.KEYCODE_FORWARD_DEL, 0, false, false, actionDown))
    assertEquals(DesktopKeyboardPolicy.HardwareAction.NamedKey("Enter"),
      DesktopKeyboardPolicy.hardwareAction(DesktopKeyboardPolicy.KEYCODE_ENTER, 0, false, false, actionDown))
    assertEquals(DesktopKeyboardPolicy.HardwareAction.NamedKey("Enter"),
      DesktopKeyboardPolicy.hardwareAction(DesktopKeyboardPolicy.KEYCODE_NUMPAD_ENTER, 0, false, false, actionDown))
  }

  @Test
  fun hardwarePrintableKeysMapToTextOnce() {
    assertEquals(DesktopKeyboardPolicy.HardwareAction.Text("A"),
      DesktopKeyboardPolicy.hardwareAction(29, 65, false, false, actionDown))
    assertEquals(DesktopKeyboardPolicy.HardwareAction.ConsumeUp,
      DesktopKeyboardPolicy.hardwareAction(29, 65, false, false, actionUp))
    assertEquals(DesktopKeyboardPolicy.HardwareAction.Text("中"),
      DesktopKeyboardPolicy.hardwareAction(0, 0x4e2d, false, false, actionDown))
    assertEquals(DesktopKeyboardPolicy.HardwareAction.Ignore,
      DesktopKeyboardPolicy.hardwareAction(29, 0, false, false, actionDown))
    assertEquals(DesktopKeyboardPolicy.HardwareAction.Ignore,
      DesktopKeyboardPolicy.hardwareAction(29, 65, true, false, actionDown))
    assertEquals(DesktopKeyboardPolicy.HardwareAction.Ignore,
      DesktopKeyboardPolicy.hardwareAction(29, 65, false, true, actionDown))
  }

  @Test
  fun hardwareRepeatsAndFastDistinctKeysAllDispatch() {
    // Holding backspace repeats KEYCODE_DEL: every down is dispatched, no
    // 40 ms window drops any of them.
    val repeats = (1..5).map {
      DesktopKeyboardPolicy.hardwareAction(DesktopKeyboardPolicy.KEYCODE_DEL, 0, false, false, actionDown)
    }
    assertEquals(5, repeats.count { it == DesktopKeyboardPolicy.HardwareAction.NamedKey("Backspace") })
    // Fast distinct keys keep their own actions.
    val mixed = listOf(
      DesktopKeyboardPolicy.hardwareAction(DesktopKeyboardPolicy.KEYCODE_DEL, 0, false, false, actionDown),
      DesktopKeyboardPolicy.hardwareAction(DesktopKeyboardPolicy.KEYCODE_ENTER, 0, false, false, actionDown),
      DesktopKeyboardPolicy.hardwareAction(DesktopKeyboardPolicy.KEYCODE_DEL, 0, false, false, actionDown),
    )
    assertEquals(listOf(
      DesktopKeyboardPolicy.HardwareAction.NamedKey("Backspace"),
      DesktopKeyboardPolicy.HardwareAction.NamedKey("Enter"),
      DesktopKeyboardPolicy.HardwareAction.NamedKey("Backspace"),
    ), mixed)
  }

  @Test
  fun localPreeditDeleteKeepsRemainingTextAndSendsNothing() {
    // composing "abc" (0..3) with cursor at 3: deleting one preedit character is
    // local only, and a later commit must send just the remaining "ab".
    assertEquals(emptyList<String>(), DesktopKeyboardPolicy.deleteKeys(1, 0, 3, 0, 3, 3))
    assertEquals(emptyList<String>(), DesktopKeyboardPolicy.deleteKeys(2, 0, 3, 0, 3, 3))
    assertEquals("ab", DesktopKeyboardPolicy.committedText("ab", -1, -1, false))
  }

  @Test
  fun imeDeleteThenIndependentHardwareDeleteBothReachRemote() {
    // The IME delete is one request; the hardware key event is a separate
    // request and must reach the remote too (no boolean or timer dedupe).
    assertEquals(listOf("Backspace"), DesktopKeyboardPolicy.deleteKeys(1, 0, 0, -1, -1, 0))
    assertEquals(DesktopKeyboardPolicy.HardwareAction.NamedKey("Backspace"),
      DesktopKeyboardPolicy.hardwareAction(DesktopKeyboardPolicy.KEYCODE_DEL, 0, false, false, actionDown))
    assertEquals(listOf("Delete"), DesktopKeyboardPolicy.deleteKeys(0, 1, 0, -1, -1, 0))
    assertEquals(DesktopKeyboardPolicy.HardwareAction.NamedKey("Delete"),
      DesktopKeyboardPolicy.hardwareAction(DesktopKeyboardPolicy.KEYCODE_FORWARD_DEL, 0, false, false, actionDown))
  }
}
