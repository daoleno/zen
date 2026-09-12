package expo.modules.zenremotedesktop

/**
 * Pure decision layer for the commit-aware remote keyboard.
 *
 * The Android view delegates every commit/edit decision here so the exact
 * remote output can be unit tested without a framework: committed text,
 * composing text, cancel, paste, delete counts and hardware key mapping.
 * No Android types are referenced on purpose.
 */
internal object DesktopKeyboardPolicy {
  const val KEYCODE_DEL = 67
  const val KEYCODE_ENTER = 66
  const val KEYCODE_FORWARD_DEL = 112
  const val KEYCODE_NUMPAD_ENTER = 160

  const val KEY_BACKSPACE = "Backspace"
  const val KEY_DELETE = "Delete"
  const val KEY_ENTER = "Enter"

  sealed interface HardwareAction {
    data class Text(val value: String) : HardwareAction
    data class NamedKey(val key: String) : HardwareAction
    /** The key down was already dispatched; consume the matching key up. */
    object ConsumeUp : HardwareAction
    object Ignore : HardwareAction
  }

  /**
   * Text that becomes committed after one Editable change. A live composing
   * region is local candidate text and quiet edits (our own clear) never
   * dispatch.
   */
  fun committedText(text: String?, composingStart: Int, composingEnd: Int, quiet: Boolean): String? {
    if (quiet || text.isNullOrEmpty()) return null
    if (composingStart >= 0 && composingEnd > composingStart) return null
    return text
  }

  /**
   * Text that becomes committed when the IME finishes composing. The finishe
   * preedit is real text from the user's point of view, so it is dispatched
   * once; a span-only finish never triggers a TextWatcher.
   */
  fun finishedComposingText(text: String?, composingStart: Int, composingEnd: Int): String? {
    if (text.isNullOrEmpty()) return null
    if (composingStart < 0 || composingEnd <= composingStart || composingEnd > text.length) return null
    return text.substring(composingStart, composingEnd)
  }

  /**
   * Remote named keys for one deleteSurroundingText call. Characters inside a
   * composing region are preedit and are deleted locally only; every requested
   * deletion outside it maps to exactly one remote key, so a three-character
   * delete sends three backspaces and never fewer.
   */
  fun deleteKeys(
    beforeLength: Int,
    afterLength: Int,
    cursor: Int,
    composingStart: Int,
    composingEnd: Int,
    length: Int,
  ): List<String> {
    val safeLength = length.coerceAtLeast(0)
    val safeCursor = cursor.coerceIn(0, safeLength)
    val before = beforeLength.coerceAtLeast(0)
    val after = afterLength.coerceAtLeast(0)
    val beforeFrom = (safeCursor - before).coerceAtLeast(0)
    val beforeTo = safeCursor
    val afterFrom = safeCursor
    val afterTo = (safeCursor + after).coerceAtMost(safeLength)
    val keys = ArrayList<String>(before + after)
    repeat((before - overlap(beforeFrom, beforeTo, composingStart, composingEnd)).coerceAtLeast(0)) { keys.add(KEY_BACKSPACE) }
    repeat((after - overlap(afterFrom, afterTo, composingStart, composingEnd)).coerceAtLeast(0)) { keys.add(KEY_DELETE) }
    return keys
  }

  /**
   * Hardware and injected key mapping. Printable keys become one text event on
   * key down; named editing keys become one named event on key down; the
   * matching key up is consumed so the local editor cannot edit or repeat.
   * There is no time window: every distinct or repeated key dispatches.
   */
  fun hardwareAction(keyCode: Int, unicodeChar: Int, ctrl: Boolean, alt: Boolean, action: Int): HardwareAction {
    val down = action == 0 // KeyEvent.ACTION_DOWN
    val up = action == 1 // KeyEvent.ACTION_UP
    if (!down && !up) return HardwareAction.Ignore
    val named = when (keyCode) {
      KEYCODE_DEL -> KEY_BACKSPACE
      KEYCODE_FORWARD_DEL -> KEY_DELETE
      KEYCODE_ENTER, KEYCODE_NUMPAD_ENTER -> KEY_ENTER
      else -> null
    }
    val printable = named == null && unicodeChar in 0x20..0x10ffff && unicodeChar != 0x7f && !ctrl && !alt
    if (down) {
      if (named != null) return HardwareAction.NamedKey(named)
      if (printable) return HardwareAction.Text(String(Character.toChars(unicodeChar)))
      return HardwareAction.Ignore
    }
    return if (named != null || printable) HardwareAction.ConsumeUp else HardwareAction.Ignore
  }

  private fun overlap(from: Int, to: Int, start: Int, end: Int): Int {
    if (start < 0 || end <= start || to <= from) return 0
    return (minOf(to, end) - maxOf(from, start)).coerceAtLeast(0)
  }
}
