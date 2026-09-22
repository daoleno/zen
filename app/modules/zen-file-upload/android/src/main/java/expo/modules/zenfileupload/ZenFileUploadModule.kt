package expo.modules.zenfileupload

import android.content.ContentResolver
import android.net.Uri
import android.provider.OpenableColumns
import expo.modules.kotlin.Promise
import expo.modules.kotlin.exception.Exceptions
import expo.modules.kotlin.functions.Coroutine
import expo.modules.kotlin.functions.Queues
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import java.io.BufferedInputStream
import java.io.BufferedOutputStream
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

private const val UPLOAD_BUFFER_BYTES = 1024 * 1024
private const val PROGRESS_INTERVAL_MS = 200L
private const val CONNECT_TIMEOUT_MS = 60_000
private const val READ_TIMEOUT_MS = 60_000

class NativeDownloadRequest : expo.modules.kotlin.records.Record {
    @expo.modules.kotlin.records.Field var downloadId: String = ""
    @expo.modules.kotlin.records.Field var url: String = ""
    @expo.modules.kotlin.records.Field var destinationUri: String = ""
    @expo.modules.kotlin.records.Field var expectedSize: Long? = null
    @expo.modules.kotlin.records.Field var maxBytes: Long = 0
    @expo.modules.kotlin.records.Field var headers: Map<String, String> = emptyMap()
}

class NativeUploadRequest : expo.modules.kotlin.records.Record {
    @expo.modules.kotlin.records.Field var uploadId: String = ""
    @expo.modules.kotlin.records.Field var url: String = ""
    @expo.modules.kotlin.records.Field var fileUri: String = ""
    @expo.modules.kotlin.records.Field var expectedSize: Long? = null
    @expo.modules.kotlin.records.Field var method: String = "POST"
    @expo.modules.kotlin.records.Field var headers: Map<String, String> = emptyMap()
}

class ZenFileUploadModule : Module() {
    private var pickerLimit = 8
    private val picker = PickerOwnership<Promise>()
    private val pickerCancellation = android.os.CancellationSignal()
    private val pickerReads = Executors.newSingleThreadExecutor()
    private val active = ConcurrentHashMap<String, ActiveUpload>()
    private val activeDownloads = ConcurrentHashMap<String, ActiveDownload>()

    override fun definition() = ModuleDefinition {
        Name("ZenFileUpload")
        Events("onUploadProgress")

        AsyncFunction("pickDocuments") { maxCount: Int, promise: Promise ->
            require(maxCount in 1..8) { "Choose between one and eight files." }
            picker.begin(promise)
            pickerLimit = maxCount
            try {
                appContext.throwingActivity.startActivityForResult(
                    UploadDocumentPicker.intent(), UploadDocumentPicker.REQUEST_CODE,
                )
            } catch (error: Exception) {
                if (picker.finish(promise)) {
                    promise.reject("ERR_DOCUMENT_PICK", "Could not open the file picker.", error)
                }
            }
        }.runOnQueue(Queues.MAIN)

        OnActivityResult { _, (requestCode, resultCode, intent) ->
            if (requestCode == UploadDocumentPicker.REQUEST_CODE) {
                val promise = picker.read()
                if (promise != null) {
                    val resolver = appContext.reactContext?.contentResolver
                    try {
                        pickerReads.execute {
                            try {
                                if (resolver == null) throw Exceptions.ReactContextLost()
                                val debug = (appContext.reactContext?.applicationInfo?.flags ?: 0) and
                                    android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE != 0
                                val asset = UploadDocumentPicker.readResult(resolver, resultCode, intent, pickerCancellation, debug, pickerLimit)
                                if (picker.finish(promise)) promise.resolve(asset)
                            } catch (error: Exception) {
                                if (picker.finish(promise)) promise.reject("ERR_DOCUMENT_READ", error.message, error)
                            }
                        }
                    } catch (error: java.util.concurrent.RejectedExecutionException) {
                        if (picker.finish(promise)) promise.resolve(emptyList<Any>())
                    }
                }
            }
        }

        AsyncFunction("upload") Coroutine { request: NativeUploadRequest ->
            upload(request)
        }

        Function("cancel") { uploadId: String ->
            active[uploadId]?.cancel() ?: false
        }

        AsyncFunction("download") Coroutine { request: NativeDownloadRequest ->
            download(request)
        }

        Function("cancelDownload") { downloadId: String ->
            activeDownloads[downloadId]?.cancel() ?: false
        }

        OnDestroy {
            picker.destroy()?.resolve(emptyList<Any>())
            pickerCancellation.cancel()
            pickerReads.shutdownNow()
            active.values.forEach { it.cancel() }
            active.clear()
            activeDownloads.values.forEach { it.cancel() }
            activeDownloads.clear()
        }
    }

    private fun upload(request: NativeUploadRequest): Map<String, Any> {
        validateRequest(request)
        val context = appContext.reactContext ?: throw Exceptions.ReactContextLost()
        val uri = Uri.parse(request.fileUri)
        val contentLength = resolveContentLength(context.contentResolver, uri, request.expectedSize)
        val connection = (URL(request.url).openConnection() as HttpURLConnection).apply {
            connectTimeout = CONNECT_TIMEOUT_MS
            readTimeout = READ_TIMEOUT_MS
            requestMethod = request.method.uppercase()
            doOutput = true
            useCaches = false
            instanceFollowRedirects = false
            request.headers.forEach { (name, value) ->
                if (!name.equals("Content-Length", ignoreCase = true)) {
                    setRequestProperty(name, value)
                }
            }
            setRequestProperty("X-Zen-Upload-Transport", "android-native-v1")
            if (contentLength >= 0) {
                setFixedLengthStreamingMode(contentLength)
            } else {
                setChunkedStreamingMode(UPLOAD_BUFFER_BYTES)
            }
        }
        val operation = ActiveUpload(connection)
        check(active.putIfAbsent(request.uploadId, operation) == null) {
            "An upload with this ID is already active."
        }

        try {
            openInput(context.contentResolver, uri).use { rawInput ->
                BufferedInputStream(rawInput, UPLOAD_BUFFER_BYTES).use { input ->
                    BufferedOutputStream(connection.outputStream, UPLOAD_BUFFER_BYTES).use { output ->
                        val buffer = ByteArray(UPLOAD_BUFFER_BYTES)
                        var sent = 0L
                        var lastProgressAt = 0L
                        while (true) {
                            operation.requireActive()
                            val read = input.read(buffer)
                            if (read < 0) {
                                break
                            }
                            output.write(buffer, 0, read)
                            sent += read
                            val now = System.currentTimeMillis()
                            if (now - lastProgressAt >= PROGRESS_INTERVAL_MS) {
                                lastProgressAt = now
                                emitProgress(request.uploadId, sent, contentLength)
                            }
                        }
                        output.flush()
                        emitProgress(request.uploadId, sent, if (contentLength >= 0) contentLength else sent)
                    }
                }
            }

            operation.requireActive()
            val status = connection.responseCode
            val responseStream = if (status >= 400) connection.errorStream else connection.inputStream
            val body = responseStream?.bufferedReader()?.use { it.readText() } ?: ""
            val headers = connection.headerFields
                .filterKeys { it != null }
                .mapValues { (_, values) -> values.firstOrNull().orEmpty() }
            return mapOf("body" to body, "status" to status, "headers" to headers)
        } catch (error: Throwable) {
            if (operation.cancelled.get()) {
                throw IllegalStateException("Attachment upload cancelled.", error)
            }
            throw IllegalStateException("Could not upload the attachment.", error)
        } finally {
            active.remove(request.uploadId, operation)
            connection.disconnect()
        }
    }

    private fun emitProgress(uploadId: String, bytesSent: Long, totalBytes: Long) {
        sendEvent(
            "onUploadProgress",
            mapOf(
                "uploadId" to uploadId,
                "bytesSent" to bytesSent,
                "totalBytes" to totalBytes,
            ),
        )
    }

    private fun download(request: NativeDownloadRequest): Map<String, Any> {
        validateDownloadRequest(request)
        val context = appContext.reactContext ?: throw Exceptions.ReactContextLost()
        val destination = Uri.parse(request.destinationUri)
        val connection = (URL(request.url).openConnection() as HttpURLConnection).apply {
            connectTimeout = CONNECT_TIMEOUT_MS
            readTimeout = READ_TIMEOUT_MS
            requestMethod = "GET"
            doInput = true
            useCaches = false
            instanceFollowRedirects = false
            request.headers.forEach { (name, value) ->
                setRequestProperty(name, value)
            }
            setRequestProperty("X-Zen-Download-Transport", "android-native-v1")
        }
        val operation = ActiveDownload(connection)
        check(activeDownloads.putIfAbsent(request.downloadId, operation) == null) {
            "A download with this ID is already active."
        }

        try {
            val status = connection.responseCode
            if (status !in 200..299) {
                throw IllegalStateException("Session file download failed (HTTP $status).")
            }
            val announcedSize = connection.contentLengthLong
            if (announcedSize > request.maxBytes) {
                throw IllegalStateException("Session file download exceeded the ${request.maxBytes} byte limit.")
            }

            var received = 0L
            BufferedInputStream(connection.inputStream, UPLOAD_BUFFER_BYTES).use { input ->
                BufferedOutputStream(openOutput(context.contentResolver, destination), UPLOAD_BUFFER_BYTES).use { output ->
                    val buffer = ByteArray(UPLOAD_BUFFER_BYTES)
                    while (true) {
                        operation.requireActive()
                        val read = input.read(buffer)
                        if (read < 0) {
                            break
                        }
                        received += read
                        if (received > request.maxBytes) {
                            throw IllegalStateException("Session file download exceeded the ${request.maxBytes} byte limit.")
                        }
                        output.write(buffer, 0, read)
                    }
                    output.flush()
                }
            }

            request.expectedSize?.let { expected ->
                if (received != expected) {
                    throw IllegalStateException(
                        "Session file download was truncated (expected $expected bytes, received $received).",
                    )
                }
            }
            return mapOf("bytesWritten" to received)
        } catch (error: Throwable) {
            if (operation.cancelled.get()) {
                throw IllegalStateException("Session file download was cancelled.", error)
            }
            throw error
        } finally {
            activeDownloads.remove(request.downloadId, operation)
            connection.disconnect()
        }
    }

    private fun validateRequest(request: NativeUploadRequest) {
        require(request.uploadId.isNotBlank()) { "Upload ID is required." }
        require(request.method.equals("POST", ignoreCase = true) || request.method.equals("PUT", ignoreCase = true)) {
            "Upload method is invalid."
        }
        val target = URL(request.url)
        require(target.protocol == "http" || target.protocol == "https") { "Upload URL is invalid." }
        val source = Uri.parse(request.fileUri)
        require(source.scheme == ContentResolver.SCHEME_CONTENT || source.scheme == ContentResolver.SCHEME_FILE) {
            "Upload file URI is invalid."
        }
        request.expectedSize?.let { require(it >= 0) { "Upload size is invalid." } }
    }

    private fun validateDownloadRequest(request: NativeDownloadRequest) {
        require(request.downloadId.isNotBlank()) { "Download ID is required." }
        val target = URL(request.url)
        require(target.protocol == "http" || target.protocol == "https") { "Download URL is invalid." }
        val destination = Uri.parse(request.destinationUri)
        require(destination.scheme == ContentResolver.SCHEME_CONTENT || destination.scheme == ContentResolver.SCHEME_FILE) {
            "Download destination URI is invalid."
        }
        require(request.maxBytes > 0) { "Download byte limit is invalid." }
        request.expectedSize?.let {
            require(it >= 0 && it <= request.maxBytes) { "Download size is invalid." }
        }
    }
}

private class ActiveUpload(private val connection: HttpURLConnection) {
    val cancelled = AtomicBoolean(false)

    fun cancel(): Boolean {
        val changed = cancelled.compareAndSet(false, true)
        connection.disconnect()
        return changed
    }

    fun requireActive() {
        check(!cancelled.get()) { "Attachment upload cancelled." }
    }
}

private class ActiveDownload(private val connection: HttpURLConnection) {
    val cancelled = AtomicBoolean(false)

    fun cancel(): Boolean {
        val changed = cancelled.compareAndSet(false, true)
        connection.disconnect()
        return changed
    }

    fun requireActive() {
        check(!cancelled.get()) { "Session file download was cancelled." }
    }
}

private fun openInput(resolver: ContentResolver, uri: Uri) = when (uri.scheme) {
    ContentResolver.SCHEME_FILE -> File(requireNotNull(uri.path)).inputStream()
    ContentResolver.SCHEME_CONTENT -> resolver.openInputStream(uri)
        ?: throw IllegalStateException("Could not open the selected file.")
    else -> throw IllegalArgumentException("Upload file URI is invalid.")
}

private fun openOutput(resolver: ContentResolver, uri: Uri) = when (uri.scheme) {
    ContentResolver.SCHEME_FILE -> File(requireNotNull(uri.path)).outputStream()
    ContentResolver.SCHEME_CONTENT -> resolver.openOutputStream(uri, "w")
        ?: throw IllegalStateException("Could not open the selected download destination.")
    else -> throw IllegalArgumentException("Download destination URI is invalid.")
}

private fun resolveContentLength(resolver: ContentResolver, uri: Uri, expectedSize: Long?): Long {
    if (expectedSize != null) {
        return expectedSize
    }
    if (uri.scheme == ContentResolver.SCHEME_FILE) {
        return File(requireNotNull(uri.path)).length()
    }
    // Advisory metadata must not veto a readable stream accepted by the picker.
    try {
        resolver.query(uri, arrayOf(OpenableColumns.SIZE), null, null, null)?.use { cursor ->
            if (cursor.moveToFirst()) {
                val index = cursor.getColumnIndex(OpenableColumns.SIZE)
                if (index >= 0 && !cursor.isNull(index)) {
                    return cursor.getLong(index).takeIf { it >= 0 } ?: -1
                }
            }
        }
    } catch (error: android.os.OperationCanceledException) {
        throw error
    } catch (_: Exception) {
        // Unknown size uses chunked transfer; opening still enforces permission.
    }
    return -1
}
