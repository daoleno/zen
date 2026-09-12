package expo.modules.zenremotedesktop

/**
 * Test-only JNI probe implemented by the inert core in
 * libzen_moonlight_test.so. Never shipped in the app.
 */
object MoonlightTestProbe {
  external fun reset()
  external fun startCount(): Int
  external fun stopCount(): Int
  external fun interruptCount(): Int
  external fun startEntered(): Boolean
  external fun setStartResult(result: Int)
  external fun setStartBlocks(blocks: Boolean)
  external fun emitTerminated(errorCode: Int)
  external fun emitDecodeUnit(payload: ByteArray, frameType: Int, presentationTimeUs: Long, declaredLength: Int)
  external fun lastUtf8Bytes(): ByteArray
  external fun lastKeyCode(): Short
}
