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
            val assets = UploadDocumentPicker.readResult(contentResolver, resultCode, data)
            if (assets.isEmpty()) "CANCEL: no attachment" else "OK: ${assets.size} selected; ${assets.count { it["selectionError"] != null }} failed"

        } catch (error: Exception) { "ERROR: ${error.message}" }
        android.util.Log.i("ZenPickerFixture", outcome)
        setContentView(TextView(this).apply { text = outcome; textSize = 20f })
    }
}
