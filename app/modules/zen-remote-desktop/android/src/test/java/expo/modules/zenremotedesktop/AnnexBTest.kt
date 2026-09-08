package expo.modules.zenremotedesktop

import org.junit.Assert.*
import org.junit.Test

class AnnexBTest {
  @Test fun splitsMixedPrefixesWithoutIncludingStartCodes() {
    val units = AnnexB.units(byteArrayOf(0, 0, 0, 1, 103, 42, 0, 0, 1, 104, 21, 0, 0, 1, 101, 7))
    assertEquals(3, units.size)
    assertArrayEquals(byteArrayOf(103, 42), units[0])
    assertArrayEquals(byteArrayOf(104, 21), units[1])
    assertArrayEquals(byteArrayOf(101, 7), units[2])
  }

  @Test fun ignoresEmptyAndTruncatedPrefixes() {
    for (data in listOf(byteArrayOf(), byteArrayOf(0, 0), byteArrayOf(0, 0, 1))) {
      assertTrue(AnnexB.units(data).isEmpty())
    }
  }

  @Test(expected = IllegalArgumentException::class)
  fun rejectsOversizedAccessUnit() { AnnexB.units(ByteArray(4 * 1024 * 1024 + 1)) }

  @Test(expected = IllegalArgumentException::class)
  fun boundsNalUnitCount() { AnnexB.units(ByteArray(4097 * 4) { if (it % 4 == 2) 1 else 0 }) }
}
