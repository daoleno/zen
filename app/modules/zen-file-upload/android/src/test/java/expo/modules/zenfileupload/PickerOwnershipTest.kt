package expo.modules.zenfileupload

import org.junit.Assert.*
import org.junit.Test

class PickerOwnershipTest {
    @Test fun ownsPromiseUntilReadFinishes() {
        val owner = PickerOwnership<Any>()
        val first = Any()
        owner.begin(first)
        assertSame(first, owner.read())
        assertNull(owner.read()) // duplicate activity result
        try { owner.begin(Any()); fail("concurrent picker accepted") } catch (_: IllegalStateException) {}
        assertTrue(owner.finish(first))
        val second = Any()
        owner.begin(second)
        assertFalse(owner.finish(first))
        assertSame(second, owner.read())
    }

    @Test fun destructionSettlesPendingReadOnlyOnce() {
        val owner = PickerOwnership<Any>()
        val pending = Any()
        owner.begin(pending)
        owner.read()
        assertSame(pending, owner.destroy())
        assertFalse(owner.finish(pending))
        assertNull(owner.read())
        assertNull(owner.destroy())
        try { owner.begin(Any()); fail("destroyed picker accepted") } catch (_: IllegalStateException) {}
    }
}
