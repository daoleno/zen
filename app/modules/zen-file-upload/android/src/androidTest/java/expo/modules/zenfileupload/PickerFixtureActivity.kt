package expo.modules.zenfileupload

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.widget.TextView

/** Minimal real ACTION_OPEN_DOCUMENT host using the production result handler. */
class PickerFixtureActivity : Activity() {
    override fun onCreate(state: Bundle?) {
        super.onCreate(state)
        startActivityForResult(UploadDocumentPicker.intent(), UploadDocumentPicker.REQUEST_CODE)
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != UploadDocumentPicker.REQUEST_CODE) return
        val outcome = try {
            val asset = UploadDocumentPicker.readResult(contentResolver, resultCode, data)
            if (asset == null) "CANCEL: no attachment" else {
                val bytes = contentResolver.openInputStream(android.net.Uri.parse(asset["uri"] as String))!!.use { it.readBytes() }
                "OK: $asset\n${String(bytes, Charsets.UTF_8)}"
            }
        } catch (error: Exception) { "ERROR: ${error.message}" }
        android.util.Log.i("ZenPickerFixture", outcome)
        setContentView(TextView(this).apply { text = outcome; textSize = 20f })
    }
}
