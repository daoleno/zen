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

    // One-shot attachment import, including apps that don't expose DocumentsProvider.
    // Keep every user-selected URI and its original activity read grant.
    fun intent() = Intent.createChooser(Intent(Intent.ACTION_GET_CONTENT).apply {
        addCategory(Intent.CATEGORY_OPENABLE)
        type = "*/*"
        putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    }, "Attach files")

    fun readResult(resolver: ContentResolver, resultCode: Int, intent: Intent?, signal: CancellationSignal? = null, debug: Boolean = false, maxCount: Int = 8): List<Map<String, Any?>> {
        fun diagnostic(message: String) {
            // Structural facts only; never URI, metadata values or exception messages.
            if (debug) Log.d("ZenDocumentPicker", message)
        }
        val clip = intent?.clipData
        diagnostic("result=$resultCode data=${intent?.data != null} clipCount=${clip?.itemCount ?: 0} flags=${intent?.flags ?: 0} readGrant=${(intent?.flags ?: 0) and Intent.FLAG_GRANT_READ_URI_PERMISSION != 0} mainThread=${android.os.Looper.myLooper() == android.os.Looper.getMainLooper()}")
        if (resultCode == Activity.RESULT_CANCELED) return emptyList()
        if (resultCode != Activity.RESULT_OK) throw failure("RESULT", "Could not select this file. Try again.")
        val uris = if (clip != null && clip.itemCount > 0) {
            (0 until clip.itemCount).map { clip.getItemAt(it).uri ?: throw failure("RESULT", "The picker returned an invalid file.") }
        } else listOf(intent?.data ?: throw failure("RESULT", "No file was selected. Choose files again."))
        if (uris.size > maxCount) throw failure("LIMIT", "Choose up to $maxCount files. Remove an attachment to make room.")
        return uris.map { uri ->
            signal?.throwIfCanceled()
            diagnostic("uriContent=${uri.scheme == ContentResolver.SCHEME_CONTENT} authorityPresent=${!uri.authority.isNullOrBlank()}")
            try { read(resolver, uri, signal, debug, ::diagnostic) }
            catch (error: OperationCanceledException) { throw error }
            catch (error: Exception) {
                mapOf("uri" to uri.toString(), "name" to "Unreadable file", "mimeType" to "application/octet-stream", "size" to null, "selectionError" to error.message)
            }
        }
    }

    private fun read(resolver: ContentResolver, uri: Uri, signal: CancellationSignal?, debug: Boolean, diagnostic: (String) -> Unit): Map<String, Any?> {
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
            } ?: throw NoStreamException()
        } catch (error: Exception) {
            // Expo JSI drops native causes. Include only bounded structural details
            // in Debug alerts so a phone screenshot suffices without USB/logcat.
            val detail = if (debug) {
                val authority = uri.authority?.takeIf { it.matches(Regex("[A-Za-z0-9_.-]{1,120}")) } ?: "nonstandard"
                val classes = generateSequence<Throwable>(error) { it.cause }.take(4).joinToString(" > ") {
                    val name = it.javaClass.simpleName.takeIf { value -> value.matches(Regex("[A-Za-z0-9_$]{1,80}")) } ?: "Exception"
                    if (it is android.system.ErrnoException) "$name(errno=${it.errno})" else name
                }
                "provider=$authority; exception=$classes"
            } else null
            if (detail != null) diagnostic("stage=$stage $detail")
            val reason = when (error) {
                is SecurityException -> "Access was denied. Select the file again."
                is OperationCanceledException -> "File selection was canceled."
                is NoStreamException -> "The document provider returned no stream. Select the file again."
                is java.io.FileNotFoundException -> "The document provider could not open the selected content."
                is UnsupportedOperationException -> "The document provider does not support opening this content."
                is android.os.DeadObjectException -> "The document provider stopped. Select the file again."
                else -> "The document provider could not read it."
            }
            val code = if (error is SecurityException) "$stage-PERMISSION" else stage
            val failure = failure(code, "Cannot read this file. $reason", error)
            throw if (detail == null) failure else IOException("${failure.message}\n$detail", error)
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

    private class NoStreamException : IOException("Document provider returned no descriptor")
}
