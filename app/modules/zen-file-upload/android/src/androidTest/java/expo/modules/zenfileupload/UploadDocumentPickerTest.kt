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
        return UploadDocumentPicker.readResult(instrumentation.context.contentResolver, Activity.RESULT_OK, Intent().setData(uri)).single()
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

    @Test fun testUnreadableDirectoryAndOpaqueInvalidURIReturnPerFileErrors() {
        for (id in listOf("unreadable", "directory", ":")) {
            assertTrue((select(id)!!["selectionError"] as String).startsWith("Cannot read this file."))
        }
        assertNull(select("query-error")!!["selectionError"])
    }

    @Test fun testMultipleClipDataPreservesOrderAndPartialFailure() {
        val resolver = instrumentation.context.contentResolver
        val uris = listOf("normal", "unreadable", "unknown").map { DocumentsContract.buildDocumentUri("expo.modules.zenfileupload.fixture", it) }
        val clip = android.content.ClipData.newRawUri("files", uris[0])
        uris.drop(1).forEach { clip.addItem(android.content.ClipData.Item(it)) }
        val intent = Intent().apply { clipData = clip; addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION) }
        val assets = UploadDocumentPicker.readResult(resolver, Activity.RESULT_OK, intent)
        assertEquals(uris.map { it.toString() }, assets.map { it["uri"] })
        assertNull(assets[0]["selectionError"])
        assertNotNull(assets[1]["selectionError"])
        assertNull(assets[2]["selectionError"])
        try { UploadDocumentPicker.readResult(resolver, Activity.RESULT_OK, intent, maxCount = 2); fail("excess accepted") } catch (_: java.io.IOException) {}
        val chooser = UploadDocumentPicker.intent()
        val inner = chooser.getParcelableExtra<Intent>(Intent.EXTRA_INTENT)!!
        assertTrue(inner.getBooleanExtra(Intent.EXTRA_ALLOW_MULTIPLE, false))
    }

    @Test fun testUnknownAndEmptyFileSizes() {
        assertNull(select("unknown")!!["size"])
        assertNull(select("null-size")!!["size"])
        assertEquals(0L, select("zero")!!["size"])
    }

    @Test fun testInvalidSchemeAndCancelledRead() {
        val invalid = UploadDocumentPicker.readResult(instrumentation.context.contentResolver, Activity.RESULT_OK,
            Intent().setData(Uri.parse("file:///not-a-provider")))
        assertNotNull(invalid.single()["selectionError"])
        val signal = android.os.CancellationSignal().apply { cancel() }
        try {
            UploadDocumentPicker.readResult(instrumentation.context.contentResolver, Activity.RESULT_OK,
                Intent().setData(DocumentsContract.buildDocumentUri("expo.modules.zenfileupload.fixture", "normal")), signal)
            fail("cancelled metadata read accepted")
        } catch (_: android.os.OperationCanceledException) {}
    }

    @Test fun testCancelAndMissingResult() {
        assertTrue(UploadDocumentPicker.readResult(instrumentation.context.contentResolver, Activity.RESULT_CANCELED, null).isEmpty())
        try {
            UploadDocumentPicker.readResult(instrumentation.context.contentResolver, Activity.RESULT_OK, null)
            fail("missing URI must fail")
        } catch (expected: java.io.IOException) { assertTrue(expected.message!!.startsWith("No file")) }
    }
}
