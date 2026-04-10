package expo.modules.zenterminalvt

import android.content.Context
import android.os.Build
import android.os.Bundle
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicInteger

class ZenTerminalVtModule : Module() {
    private val terminalHandles = ConcurrentHashMap<Int, Long>()
    private val nextHandleId = AtomicInteger(1)

    private fun getPrefs() =
        appContext.reactContext?.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    private fun setBreadcrumb(stage: String, operation: String, detail: String = "") {
        getPrefs()?.edit()
            ?.putString(KEY_STAGE, stage)
            ?.putString(KEY_OPERATION, operation)
            ?.putString(KEY_DETAIL, detail)
            ?.putLong(KEY_TIMESTAMP_MS, System.currentTimeMillis())
            ?.putString(KEY_ABI, Build.SUPPORTED_ABIS.joinToString(","))
            ?.putString(KEY_MODEL, Build.MODEL ?: "")
            ?.putString(KEY_BRAND, Build.BRAND ?: "")
            ?.putInt(KEY_SDK_INT, Build.VERSION.SDK_INT)
            ?.commit()
    }

    private fun clearBreadcrumb() {
        getPrefs()?.edit()?.clear()?.commit()
    }

    private fun getBreadcrumb(): Map<String, Any?> {
        val prefs = getPrefs() ?: return emptyMap()
        val stage = prefs.getString(KEY_STAGE, null) ?: return emptyMap()

        return mapOf(
            "stage" to stage,
            "operation" to prefs.getString(KEY_OPERATION, "")!!,
            "detail" to prefs.getString(KEY_DETAIL, "")!!,
            "timestampMs" to prefs.getLong(KEY_TIMESTAMP_MS, 0L),
            "abi" to prefs.getString(KEY_ABI, "")!!,
            "model" to prefs.getString(KEY_MODEL, "")!!,
            "brand" to prefs.getString(KEY_BRAND, "")!!,
            "sdkInt" to prefs.getInt(KEY_SDK_INT, 0),
        )
    }

    private inline fun <T> runWithBreadcrumb(
        operation: String,
        detail: String = "",
        block: () -> T,
    ): T {
        setBreadcrumb("before", operation, detail)
        val result = block()
        setBreadcrumb("after", operation, detail)
        return result
    }

    private fun createHandleId(nativeHandle: Long): Int {
        if (nativeHandle == 0L) {
            return 0
        }

        while (true) {
            val handleId = nextHandleId.getAndUpdate { current ->
                if (current == Int.MAX_VALUE) 1 else current + 1
            }
            if (handleId == 0) {
                continue
            }
            if (terminalHandles.putIfAbsent(handleId, nativeHandle) == null) {
                return handleId
            }
        }
    }

    private fun getNativeHandle(handleId: Int): Long {
        return terminalHandles[handleId]
            ?: throw IllegalArgumentException("Unknown terminal handle id: $handleId")
    }

    private fun removeNativeHandle(handleId: Int): Long? {
        return terminalHandles.remove(handleId)
    }

    private fun buildSnapshotBundle(nativeState: Map<String, Any?>): Bundle {
        val bundle = Bundle(nativeState.size)

        for ((key, value) in nativeState) {
            when (value) {
                null -> bundle.putString(key, null)
                is String -> bundle.putString(key, value)
                is Int -> bundle.putInt(key, value)
                is Boolean -> bundle.putBoolean(key, value)
            }
        }

        return bundle
    }

    private fun readRenderSnapshot(handleId: Int): Bundle {
        ensureLoaded()
        return buildSnapshotBundle(nativeGetRenderSnapshot(getNativeHandle(handleId)))
    }

    override fun definition() = ModuleDefinition {
        Name("ZenTerminalVt")

        Function("createTerminal") { cols: Int, rows: Int ->
            runWithBreadcrumb("createTerminal", "cols=$cols rows=$rows") {
                ensureLoaded()
                createHandleId(nativeCreateTerminal(cols, rows))
            }
        }

        Function("destroyTerminal") { handleId: Int ->
            runWithBreadcrumb("destroyTerminal", "handleId=$handleId") {
                ensureLoaded()
                val nativeHandle = removeNativeHandle(handleId) ?: return@runWithBreadcrumb
                nativeDestroyTerminal(nativeHandle)
            }
        }

        Function("writeData") { handleId: Int, data: String ->
            val preview = data.take(32)
                .replace("\r", "\\r")
                .replace("\n", "\\n")
            runWithBreadcrumb(
                "writeData",
                "handleId=$handleId len=${data.length} preview=$preview",
            ) {
                ensureLoaded()
                nativeWriteData(getNativeHandle(handleId), data)
            }
        }

        Function("resize") { handleId: Int, cols: Int, rows: Int, cellWidth: Float, cellHeight: Float ->
            runWithBreadcrumb(
                "resize",
                "handleId=$handleId cols=$cols rows=$rows cellWidth=$cellWidth cellHeight=$cellHeight",
            ) {
                ensureLoaded()
                nativeResize(getNativeHandle(handleId), cols, rows, cellWidth, cellHeight)
            }
        }

        Function("getRenderSnapshot") { handleId: Int ->
            runWithBreadcrumb("getRenderSnapshot", "handleId=$handleId") {
                readRenderSnapshot(handleId)
            }
        }

        Function("getRenderState") { handleId: Int ->
            runWithBreadcrumb("getRenderSnapshot", "handleId=$handleId") {
                readRenderSnapshot(handleId)
            }
        }

        Function("getVisibleText") { handleId: Int ->
            runWithBreadcrumb("getVisibleText", "handleId=$handleId") {
                ensureLoaded()
                nativeGetVisibleText(getNativeHandle(handleId))
            }
        }

        Function("getVisibleHtml") { handleId: Int ->
            runWithBreadcrumb("getVisibleHtml", "handleId=$handleId") {
                ensureLoaded()
                nativeGetVisibleHtml(getNativeHandle(handleId))
            }
        }

        Function("getCrashBreadcrumb") {
            getBreadcrumb()
        }

        Function("clearCrashBreadcrumb") {
            clearBreadcrumb()
        }
    }

    companion object {
        private const val PREFS_NAME = "zen_terminal_vt_diagnostics"
        private const val KEY_STAGE = "stage"
        private const val KEY_OPERATION = "operation"
        private const val KEY_DETAIL = "detail"
        private const val KEY_TIMESTAMP_MS = "timestamp_ms"
        private const val KEY_ABI = "abi"
        private const val KEY_MODEL = "model"
        private const val KEY_BRAND = "brand"
        private const val KEY_SDK_INT = "sdk_int"

        @Volatile
        private var loaded = false

        fun ensureLoaded() {
            if (loaded) return
            synchronized(this) {
                if (loaded) return
                System.loadLibrary("ghostty_vt")
                System.loadLibrary("zen_terminal_vt")
                loaded = true
            }
        }

        @JvmStatic
        external fun nativeCreateTerminal(cols: Int, rows: Int): Long

        @JvmStatic
        external fun nativeDestroyTerminal(handle: Long)

        @JvmStatic
        external fun nativeWriteData(handle: Long, data: String)

        @JvmStatic
        external fun nativeResize(handle: Long, cols: Int, rows: Int, cellWidth: Float, cellHeight: Float)

        @JvmStatic
        external fun nativeGetRenderSnapshot(handle: Long): Map<String, Any?>

        @JvmStatic
        external fun nativeGetVisibleText(handle: Long): String

        @JvmStatic
        external fun nativeGetVisibleHtml(handle: Long): String

    }
}
