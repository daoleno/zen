package expo.modules.zenremotedesktop

import org.junit.Assert.assertEquals
import org.junit.Test

class DesktopFailureTest {
  @Test fun missingEncryptionIsNotGenericAuthorization() {
    val mapped = DesktopFailure.map(403, "desktop_tls_required")
    assertEquals("disconnected", mapped.first)
    assertEquals(true, mapped.second.contains("identity-bound encrypted transport"))
    assertEquals(false, mapped.second.contains("authorization is required"))
  }

  @Test fun legacyScopeAsksForOneRePair() {
    val mapped = DesktopFailure.map(403, "desktop_scope_required")
    assertEquals("denied", mapped.first)
    assertEquals(true, mapped.second.contains("terminal access only"))
  }

  @Test fun absentHostIsSetupNotAuthorization() {
    val mapped = DesktopFailure.map(403, "host_setup_required")
    assertEquals("unsupported", mapped.first)
    assertEquals(true, mapped.second.contains("logged-in session"))
    assertEquals(true, mapped.second.contains("desktop-host"))
  }

  @Test fun revokedDeviceIsUnauthorized() {
    val mapped = DesktopFailure.map(401, "")
    assertEquals("denied", mapped.first)
    assertEquals("This device is no longer paired with the computer.", mapped.second)
  }
}
