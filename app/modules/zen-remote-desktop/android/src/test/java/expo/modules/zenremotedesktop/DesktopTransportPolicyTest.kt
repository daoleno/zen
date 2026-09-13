package expo.modules.zenremotedesktop

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DesktopTransportPolicyTest {
  @Test
  fun trustedLanIsAnAuthorizedDeploymentForSensitiveInput() {
    assertTrue(DesktopTransportPolicy.sensitiveInputAllowed("trusted-lan", tlsConnected = false, pinnedRegistered = false))
    assertTrue(DesktopTransportPolicy.sensitiveInputAllowed("tls", tlsConnected = true, pinnedRegistered = false))
    assertTrue(DesktopTransportPolicy.sensitiveInputAllowed("pinned-link", tlsConnected = false, pinnedRegistered = true))
    assertFalse(DesktopTransportPolicy.sensitiveInputAllowed("tls", tlsConnected = false, pinnedRegistered = false))
    assertFalse(DesktopTransportPolicy.sensitiveInputAllowed("pinned-link", tlsConnected = false, pinnedRegistered = false))
    assertFalse(DesktopTransportPolicy.sensitiveInputAllowed("unknown", tlsConnected = true, pinnedRegistered = true))
  }
}
