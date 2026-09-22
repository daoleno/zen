package expo.modules.zenfileupload

import android.app.Activity
import android.content.ContentResolver
import android.content.Intent
import android.net.Uri
import android.os.CancellationSignal
import android.os.OperationCanceledException
import android.provider.DocumentsContract
import android.provider.OpenableColumns
import android.util.Log
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

    fun readResult(resolver: ContentResolver, resultCode: Int, intent: Intent?, signal: CancellationSignal? = null, debug: Boolean = false): Map<String, Any?>? {
        fun diagnostic(message: String) {
            // Structural facts only; never URI, metadata values or exception messages.
            if (debug) Log.d("ZenDocumentPicker", message)
        }
        val clip = intent?.clipData
        diagnostic("result=$resultCode data=${intent?.data != null} clipCount=${clip?.itemCount ?: 0} flags=${intent?.flags ?: 0} readGrant=${(intent?.flags ?: 0) and Intent.FLAG_GRANT_READ_URI_PERMISSION != 0} mainThread=${android.os.Looper.myLooper() == android.os.Looper.getMainLooper()}")
        if (resultCode == Activity.RESULT_CANCELED) return null
        if (resultCode != Activity.RESULT_OK) throw failure("RESULT", "Could not select this file. Try again.")
        if (clip != null && (clip.itemCount != 1 || clip.getItemAt(0).uri == null)) {
            throw failure("RESULT", "Choose one file.")
        }
        val dataUri = intent?.data
        val clipUri = clip?.getItemAt(0)?.uri
        // Never silently substitute a different ClipData item.
        if (dataUri != null && clipUri != null && dataUri != clipUri) {
            throw failure("RESULT", "The picker returned conflicting files. Choose one file again.")
        }
        val uri = dataUri ?: clipUri ?: throw failure("RESULT", "No file was selected. Choose a file again.")
        diagnostic("uriContent=${uri.scheme == ContentResolver.SCHEME_CONTENT} authorityPresent=${!uri.authority.isNullOrBlank()}")
        return read(resolver, uri, signal, ::diagnostic)
    }

    private fun read(resolver: ContentResolver, uri: Uri, signal: CancellationSignal?, diagnostic: (String) -> Unit): Map<String, Any?> {
        if (uri.scheme != ContentResolver.SCHEME_CONTENT || uri.authority.isNullOrBlank()) {
            throw failure("URI", "Cannot read this file. The picker returned an invalid document URI.")
        }
        fun <T> metadata(stage: String, read: () -> T): T? = try {
            signal?.throwIfCanceled()
            read()
        } catch (error: OperationCanceledException) {
            throw error
        } catch (error: Exception) {
            diagnostic("stage=$stage exception=${error.javaClass.simpleName.take(80)} cause=${error.cause?.javaClass?.simpleName?.take(80)}")
            null
        }

        var mimeType = metadata("TYPE") { resolver.getType(uri) }
        var directory = mimeType == DocumentsContract.Document.MIME_TYPE_DIR
        var name: String? = null
        var size: Long? = null
        metadata("QUERY") {
            resolver.query(uri, null, null, null, null, signal)?.use { cursor ->
                if (cursor.moveToFirst()) {
                    // Retain directory evidence even if optional columns fail later.
                    metadata("TYPE_COLUMN") {
                        val index = cursor.getColumnIndex(DocumentsContract.Document.COLUMN_MIME_TYPE)
                        if (index >= 0 && !cursor.isNull(index)) {
                            val type = cursor.getString(index)
                            directory = directory || type == DocumentsContract.Document.MIME_TYPE_DIR
                            if (mimeType.isNullOrBlank()) mimeType = type
                        }
                    }
                    name = metadata("NAME") {
                        val index = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                        if (index >= 0 && !cursor.isNull(index)) cursor.getString(index) else null
                    }
                    size = metadata("SIZE") {
                        val index = cursor.getColumnIndex(OpenableColumns.SIZE)
                        if (index >= 0 && !cursor.isNull(index)) cursor.getLong(index).takeIf { it >= 0 } else null
                    }
                }
            }
        }
        if (directory) throw failure("DIRECTORY", "Cannot read this file. Choose a file, not a folder.")

        // Stream access is authoritative even when the provider's metadata APIs fail.
        var stage = "OPEN"
        try {
            signal?.throwIfCanceled()
            resolver.openAssetFileDescriptor(uri, "r", signal)?.use { descriptor ->
                stage = "READ"
                descriptor.createInputStream().use { it.read() }
            } ?: throw IOException("Document stream unavailable")
        } catch (error: Exception) {
            diagnostic("stage=$stage exception=${error.javaClass.simpleName.take(80)} cause=${error.cause?.javaClass?.simpleName?.take(80)}")
            val reason = when (error) {
                is SecurityException -> "Access was denied. Select the file again."
                is OperationCanceledException -> "File selection was canceled."
                else -> "The document provider could not read it."
            }
            val code = if (error is SecurityException) "$stage-PERMISSION" else stage
            throw failure(code, "Cannot read this file. $reason", error)
        }
        diagnostic("stage=READY namePresent=${!name.isNullOrBlank()} typePresent=${!mimeType.isNullOrBlank()} sizeKnown=${size != null}")
        return mapOf(
            "uri" to uri.toString(),
            "name" to (name?.takeIf { it.isNotBlank() } ?: "upload"),
            "mimeType" to (mimeType?.takeIf { it.isNotBlank() } ?: "application/octet-stream"),
            "size" to size,
        )
    }

    private fun failure(stage: String, message: String, cause: Exception? = null) =
        IOException("$message [PICK-$stage]", cause)
}
