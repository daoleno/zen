package expo.modules.zenfileupload

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.provider.DocumentsContract
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test

class UploadDocumentPickerTest {
    private val instrumentation get() = InstrumentationRegistry.getInstrumentation()
    private fun select(id: String): Map<String, Any?>? {
        val uri = DocumentsContract.buildDocumentUri("expo.modules.zenfileupload.fixture", id)
        return UploadDocumentPicker.readResult(instrumentation.context.contentResolver, Activity.RESULT_OK, Intent().setData(uri))
    }

    @Test fun testNormalUtf8TXT() {
        val asset = select("normal")!!
        assertEquals("报告.txt", asset["name"])
        assertEquals("text/plain", asset["mimeType"])
        val uri = Uri.parse(asset["uri"] as String)
        assertEquals("Hello 世界", instrumentation.context.contentResolver.openInputStream(uri)!!.bufferedReader().use { it.readText() })
    }

    @Test fun testMissingMetadataKeepsReadableOriginalURI() {
        for (id in listOf("null", "empty", "missing")) {
            val asset = select(id)!!
            assertEquals("upload", asset["name"])
            assertNull(asset["size"])
            assertEquals(DocumentsContract.buildDocumentUri("expo.modules.zenfileupload.fixture", id).toString(), asset["uri"])
        }
    }

    @Test fun testUnreadableDirectoryAndOpaqueInvalidURIReject() {
        for (id in listOf("unreadable", "directory", ":", "query-error")) {
            try { select(id); fail("must reject $id") } catch (expected: java.io.IOException) {
                assertTrue(expected.message!!.startsWith("Cannot read this file."))
            }
        }
    }

    @Test fun testUnknownAndEmptyFileSizes() {
        assertNull(select("unknown")!!["size"])
        assertNull(select("null-size")!!["size"])
        assertEquals(0L, select("zero")!!["size"])
    }

    @Test fun testInvalidSchemeAndCancelledRead() {
        try {
            UploadDocumentPicker.readResult(instrumentation.context.contentResolver, Activity.RESULT_OK,
                Intent().setData(Uri.parse("file:///not-a-provider")))
            fail("file URI accepted")
        } catch (_: java.io.IOException) {}
        val signal = android.os.CancellationSignal().apply { cancel() }
        try {
            UploadDocumentPicker.readResult(instrumentation.context.contentResolver, Activity.RESULT_OK,
                Intent().setData(DocumentsContract.buildDocumentUri("expo.modules.zenfileupload.fixture", "normal")), signal)
            fail("cancelled metadata read accepted")
        } catch (_: java.io.IOException) {}
    }

    @Test fun testCancelAndMissingResult() {
        assertNull(UploadDocumentPicker.readResult(instrumentation.context.contentResolver, Activity.RESULT_CANCELED, null))
        try {
            UploadDocumentPicker.readResult(instrumentation.context.contentResolver, Activity.RESULT_OK, null)
            fail("missing URI must fail")
        } catch (expected: java.io.IOException) { assertTrue(expected.message!!.startsWith("No file")) }
    }
}
