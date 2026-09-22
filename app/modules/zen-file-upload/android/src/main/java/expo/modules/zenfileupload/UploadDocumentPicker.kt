package expo.modules.zenfileupload

import android.app.Activity
import android.content.ContentResolver
import android.content.Intent
import android.net.Uri
import android.os.CancellationSignal
import android.provider.DocumentsContract
import android.provider.OpenableColumns
import java.io.IOException

/** Owns attachment selection only; never rewrites provider URIs or copies file bytes. */
internal object UploadDocumentPicker {
    const val REQUEST_CODE = 58341

    fun intent() = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
        addCategory(Intent.CATEGORY_OPENABLE)
        type = "*/*"
        putExtra(Intent.EXTRA_ALLOW_MULTIPLE, false)
        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    }

    fun readResult(resolver: ContentResolver, resultCode: Int, intent: Intent?, signal: CancellationSignal? = null): Map<String, Any?>? {
        if (resultCode == Activity.RESULT_CANCELED) return null
        if (resultCode != Activity.RESULT_OK) throw IOException("Could not select this file. Try again.")
        val uri = intent?.data ?: intent?.clipData?.let {
            if (it.itemCount == 1) it.getItemAt(0).uri else null
        } ?: throw IOException("No file was selected. Choose a file again.")
        return read(resolver, uri, signal)
    }

    private fun read(resolver: ContentResolver, uri: Uri, signal: CancellationSignal?): Map<String, Any?> {
        try {
            if (uri.scheme != ContentResolver.SCHEME_CONTENT || uri.authority.isNullOrBlank()) {
                throw IOException("Invalid document URI")
            }
            val mimeType = resolver.getType(uri)
            if (mimeType == DocumentsContract.Document.MIME_TYPE_DIR) {
                throw IOException("Directory selected")
            }
            var name: String? = null
            var size: Long? = null
            // A null/empty cursor or omitted optional columns does not prove the
            // stream is unavailable. Query failures, however, remain failures.
            resolver.query(uri, null, null, null, null, signal)?.use { cursor ->
                if (cursor.moveToFirst()) {
                    val nameIndex = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                    if (nameIndex >= 0 && !cursor.isNull(nameIndex)) name = cursor.getString(nameIndex)
                    val sizeIndex = cursor.getColumnIndex(OpenableColumns.SIZE)
                    if (sizeIndex >= 0 && !cursor.isNull(sizeIndex)) size = cursor.getLong(sizeIndex).takeIf { it >= 0 }
                    val typeIndex = cursor.getColumnIndex(DocumentsContract.Document.COLUMN_MIME_TYPE)
                    if (typeIndex >= 0 && cursor.getString(typeIndex) == DocumentsContract.Document.MIME_TYPE_DIR) {
                        throw IOException("Directory selected")
                    }
                }
            }
            // Prove read access (including empty files) before returning an asset.
            // Close this probe; the upload owns a fresh stream from the same URI.
            signal?.throwIfCanceled()
            resolver.openAssetFileDescriptor(uri, "r", signal)?.use { descriptor ->
                descriptor.createInputStream().use { it.read() }
            }
                ?: throw IOException("Document stream unavailable")
            return mapOf(
                "uri" to uri.toString(),
                "name" to (name?.takeIf { it.isNotBlank() } ?: "upload"),
                "mimeType" to (mimeType ?: "application/octet-stream"),
                "size" to size,
            )
        } catch (error: Exception) {
            throw IOException("Cannot read this file. Choose a file from Downloads or another document provider.", error)
        }
    }
}
