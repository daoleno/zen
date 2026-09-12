package expo.modules.zenremotedesktop

import android.content.Context
import android.os.Build
import android.text.Editable
import android.text.InputType
import android.text.Selection
import android.text.TextWatcher
import android.view.Gravity
import android.view.KeyEvent
import android.view.inputmethod.BaseInputConnection
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import android.view.inputmethod.InputConnectionWrapper
import android.view.inputmethod.InputMethodManager
import android.widget.EditText
import expo.modules.kotlin.AppContext
import expo.modules.kotlin.viewevent.EventDispatcher
import expo.modules.kotlin.views.ExpoView

/**
 * Commit-aware phone text input for the remote desktop.
 *
 * The remote desktop shows pixels, not native controls, so an ordinary React
 * Native TextInput would send IME composing text (pinyin, candidates,
 * autocorrect) into the remote window before the user commits it. This view
 * wraps an EditText whose input reports only committed text and named editing
 * keys; the composing region stays visible locally.
 *
 * All decisions live in [DesktopKeyboardPolicy] so exact remote output is
 * unit tested; this class is only the Android wiring:
 *  * soft IMEs commit through the InputConnection (commitText, paste,
 *    finishComposingText, deleteSurroundingText);
 *  * hardware and injected key events go through dispatchKeyEvent;
 *  * extract/fullscreen soft-keyboard mode is disabled so the video stays
 *    visible above the keyboard.
 */
class DesktopKeyboardView(context: Context, appContext: AppContext) : ExpoView(context, appContext) {
  private val onDesktopText by EventDispatcher()
  private val onDesktopKey by EventDispatcher()

  // Guards the watcher against our own clear() so one commit dispatch cannot
  // turn into two, and around local deletes so removed preedit never becomes
  // committed text.
  private var quietEdit = false

  private val editor = object : EditText(context) {
    override fun onCreateInputConnection(outAttrs: EditorInfo): InputConnection? {
      // Some IMEs read the flags from EditorInfo rather than the view; never
      // enter extract/fullscreen mode in landscape.
      outAttrs.imeOptions = outAttrs.imeOptions or EditorInfo.IME_FLAG_NO_EXTRACT_UI or
        EditorInfo.IME_FLAG_NO_ENTER_ACTION
      val base = super.onCreateInputConnection(outAttrs) ?: return null
      return object : InputConnectionWrapper(base, false) {
        override fun commitText(text: CharSequence?, newCursorPosition: Int): Boolean {
          // super.commitText changes the Editable; the TextWatcher below turns
          // the committed (non-composing) text into exactly one dispatch.
          return super.commitText(text, newCursorPosition)
        }

        override fun setComposingText(text: CharSequence?, newCursorPosition: Int): Boolean {
          // Composing text is not committed text. Keeping it local prevents
          // pinyin or candidates from being typed into the wrong window.
          return super.setComposingText(text, newCursorPosition)
        }

        override fun finishComposingText(): Boolean {
          val editable = text
          val start = editable?.let { BaseInputConnection.getComposingSpanStart(it) } ?: -1
          val end = editable?.let { BaseInputConnection.getComposingSpanEnd(it) } ?: -1
          val composed = DesktopKeyboardPolicy.finishedComposingText(editable?.toString(), start, end)
          quietEdit = true
          val consumed = try {
            super.finishComposingText()
          } finally {
            quietEdit = false
          }
          // Span-only finishes never trigger the TextWatcher; the finished
          // preedit is real user text and must reach the remote once.
          if (composed != null) {
            clearText()
            dispatchText(composed)
          }
          return consumed
        }

        override fun deleteSurroundingText(beforeLength: Int, afterLength: Int): Boolean {
          val editable = text
          val cursor = if (editable != null) Selection.getSelectionEnd(editable) else 0
          val start = editable?.let { BaseInputConnection.getComposingSpanStart(it) } ?: -1
          val end = editable?.let { BaseInputConnection.getComposingSpanEnd(it) } ?: -1
          val keys = DesktopKeyboardPolicy.deleteKeys(
            beforeLength, afterLength, cursor, start, end, editable?.length ?: 0)
          quietEdit = true
          val consumed = try {
            super.deleteSurroundingText(beforeLength, afterLength)
          } finally {
            quietEdit = false
          }
          // One dispatch per request. super already edited the local buffer
          // (a composing preedit shrinks to its remaining characters), so no
          // clear is posted here: a posted clear could erase a newer edit.
          keys.forEach { dispatchKey(it) }
          return consumed
        }

        override fun sendKeyEvent(event: KeyEvent): Boolean {
          // BaseInputConnection routes key events through the editor's
          // dispatchKeyEvent, which is the single dispatch point for both the
          // IME and hardware/injected keys.
          return super.sendKeyEvent(event)
        }

        override fun performEditorAction(actionCode: Int): Boolean {
          if (actionCode != EditorInfo.IME_ACTION_NONE) dispatchKey(DesktopKeyboardPolicy.KEY_ENTER)
          clearText()
          return true
        }
      }
    }

    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
      val mapped = DesktopKeyboardPolicy.hardwareAction(
        event.keyCode, event.unicodeChar, event.isCtrlPressed, event.isAltPressed, event.action)
      return when (mapped) {
        is DesktopKeyboardPolicy.HardwareAction.Text -> {
          dispatchText(mapped.value)
          true
        }
        is DesktopKeyboardPolicy.HardwareAction.NamedKey -> {
          dispatchKey(mapped.key)
          true
        }
        DesktopKeyboardPolicy.HardwareAction.ConsumeUp -> true
        DesktopKeyboardPolicy.HardwareAction.Ignore -> super.dispatchKeyEvent(event)
      }
    }
  }

  init {
    setBackgroundColor(android.graphics.Color.TRANSPARENT)
    editor.apply {
      setSingleLine(true)
      inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
      // NO_EXTRACT_UI keeps the soft keyboard docked instead of covering the
      // whole video in landscape; NO_ENTER_ACTION keeps the return key as a
      // real editing key.
      imeOptions = EditorInfo.IME_ACTION_NONE or EditorInfo.IME_FLAG_NO_ENTER_ACTION or
        EditorInfo.IME_FLAG_NO_EXTRACT_UI or EditorInfo.IME_FLAG_NO_PERSONALIZED_LEARNING
      isSaveEnabled = false
      setFreezesText(false)
      gravity = Gravity.CENTER_VERTICAL
      if (Build.VERSION.SDK_INT >= 26) {
        importantForAutofill = android.view.View.IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS
      }
      addTextChangedListener(object : TextWatcher {
        override fun beforeTextChanged(text: CharSequence?, start: Int, count: Int, after: Int) {}
        override fun onTextChanged(text: CharSequence?, start: Int, before: Int, count: Int) {}

        override fun afterTextChanged(text: Editable?) {
          val start = text?.let { BaseInputConnection.getComposingSpanStart(it) } ?: -1
          val end = text?.let { BaseInputConnection.getComposingSpanEnd(it) } ?: -1
          val committed = DesktopKeyboardPolicy.committedText(text?.toString(), start, end, quietEdit) ?: return
          quietEdit = true
          try {
            text?.clear()
          } finally {
            quietEdit = false
          }
          if (committed.isNotEmpty()) dispatchText(committed)
        }
      })
    }
    addView(editor, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
  }

  fun focusInput() {
    if (!editor.isFocused) editor.requestFocus()
    post {
      val manager = context.getSystemService(Context.INPUT_METHOD_SERVICE) as? InputMethodManager
      manager?.showSoftInput(editor, InputMethodManager.SHOW_IMPLICIT)
    }
  }

  fun clearInput() {
    clearText()
  }

  private fun clearText() {
    editor.text?.clear()
  }

  private fun dispatchText(value: String) {
    onDesktopText(mapOf("value" to value))
  }

  private fun dispatchKey(key: String) {
    onDesktopKey(mapOf("key" to key))
  }

  override fun onDetachedFromWindow() {
    // Never leave the soft keyboard attached to a destroyed remote session.
    val manager = context.getSystemService(Context.INPUT_METHOD_SERVICE) as? InputMethodManager
    manager?.hideSoftInputFromWindow(editor.windowToken, 0)
    editor.text?.clear()
    super.onDetachedFromWindow()
  }
}
