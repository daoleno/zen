package expo.modules.zenfileupload

import android.database.Cursor
import android.database.MatrixCursor
import android.os.CancellationSignal
import android.os.ParcelFileDescriptor
import android.provider.DocumentsContract.Document
import android.provider.DocumentsContract.Root
import android.provider.DocumentsProvider
import java.io.File
import java.io.FileNotFoundException

/** Test-only provider: list rows are valid, while selection metadata can be absent. */
class PickerFixtureProvider : DocumentsProvider() {
    override fun onCreate() = true
    override fun queryRoots(projection: Array<out String>?): Cursor = MatrixCursor(
        arrayOf(Root.COLUMN_ROOT_ID, Root.COLUMN_DOCUMENT_ID, Root.COLUMN_TITLE, Root.COLUMN_FLAGS),
    ).apply { addRow(arrayOf("fixture", "root", "Zen upload fixture", Root.FLAG_SUPPORTS_IS_CHILD)) }

    private val columns = arrayOf(Document.COLUMN_DOCUMENT_ID, Document.COLUMN_DISPLAY_NAME,
        Document.COLUMN_MIME_TYPE, Document.COLUMN_SIZE, Document.COLUMN_FLAGS)

    private fun row(id: String): Array<Any?> = arrayOf(id, if (id == "normal") "报告.txt" else "$id.txt",
        if (id == "root" || id == "directory") Document.MIME_TYPE_DIR else "text/plain",
        when (id) { "unknown" -> -1L; "zero" -> 0L; "null-size" -> null; else -> "Hello 世界".toByteArray().size.toLong() }, 0)

    override fun queryDocument(documentId: String, projection: Array<out String>?): Cursor? = when (documentId) {
        "null" -> null
        "empty" -> MatrixCursor(columns)
        "missing" -> MatrixCursor(arrayOf(Document.COLUMN_DOCUMENT_ID)).apply { addRow(arrayOf(documentId)) }
        "query-error" -> throw IllegalArgumentException("Fixture query failed")
        else -> MatrixCursor(columns).apply { addRow(row(documentId)) }
    }

    override fun queryChildDocuments(parentDocumentId: String, projection: Array<out String>?, sortOrder: String?): Cursor =
        MatrixCursor(columns).apply { listOf("normal", "null", "empty", "missing", "unreadable").forEach { addRow(row(it)) } }

    override fun getDocumentType(documentId: String) = if (documentId == "directory" || documentId == "root") Document.MIME_TYPE_DIR else "text/plain"

    override fun openDocument(documentId: String, mode: String, signal: CancellationSignal?): ParcelFileDescriptor {
        if (documentId == "unreadable" || documentId == ":" || documentId == "directory") throw FileNotFoundException("Fixture refuses stream")
        val file = File(context!!.cacheDir, "fixture-$documentId.txt")
        file.writeText(if (documentId == "zero") "" else "Hello 世界")
        return ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY)
    }
}
