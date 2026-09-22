package expo.modules.zenfileupload

/** Retains ownership through metadata reads; late results cannot settle a destroyed picker. */
internal class PickerOwnership<T : Any> {
    private var pending: T? = null
    private var reading = false
    private var destroyed = false

    @Synchronized fun begin(value: T) {
        check(!destroyed) { "The file picker is no longer available." }
        check(pending == null) { "A file picker is already open." }
        pending = value
    }

    @Synchronized fun read(): T? {
        if (reading || destroyed) return null
        return pending?.also { reading = true }
    }

    @Synchronized fun finish(value: T): Boolean {
        if (pending !== value) return false
        pending = null
        reading = false
        return true
    }

    @Synchronized fun destroy(): T? {
        destroyed = true
        return pending.also { pending = null }
    }
}
