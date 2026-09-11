package expo.modules.zenremotedesktop

import java.net.InetAddress
import org.junit.Assert.*
import org.junit.Test

class DesktopTransportPolicyTest {
  private val hosts = listOf("192.168.110.223", "10.1.2.3", "172.16.1.2", "100.92.1.2", "fd00::1", "fc00::1",
    "8.8.8.8", "127.0.0.1", "0.0.0.0", "169.254.1.2", "172.32.1.2", "100.128.1.2", "::1", "fe80::1", "2001:4860::8888")
    .associateWith { InetAddress.getByName(it).address }
  private fun allow(url: String, mode: String = "trusted-lan", bound: String = url.substringBefore("/desktop"),
                    source: String = bound, pin: String = "") =
    DesktopTransportPolicy.allows(url, mode, bound, source, pin) { hosts[it] }

  @Test fun privateNumericAddressesRequireAnExplicitBoundPolicy() {
    for (host in listOf("192.168.110.223", "10.1.2.3", "172.16.1.2", "100.92.1.2", "[fd00::1]", "[fc00::1]")) {
      val url = "ws://$host:9876/desktop"
      assertTrue(url, allow(url)); assertFalse(allow(url, mode = ""))
      assertFalse(allow(url, bound = "ws://192.168.110.224:9876"))
      assertFalse(allow(url, source = "wss://secure.example"))
    }
  }
  @Test fun publicLoopbackNamesAndLinkLocalAreNotTrustedLan() {
    for (host in listOf("8.8.8.8", "127.0.0.1", "0.0.0.0", "169.254.1.2", "172.32.1.2", "100.128.1.2", "localhost", "computer.local", "[::1]", "[fe80::1]", "[2001:4860::8888]")) {
      assertFalse(host, allow("ws://$host:9876/desktop"))
    }
  }
  @Test fun tlsAndPinnedLinkCannotBeDowngraded() {
    assertTrue(allow("wss://secure.example/desktop", "tls"))
    assertFalse(allow("ws://192.168.110.223/desktop", "tls"))
    val loop = "ws://127.0.0.1:32100/desktop"
    assertTrue(allow(loop, "pinned-link", source = "wss://link.example", pin = "a".repeat(64)))
    assertFalse(allow(loop, "pinned-link", source = "ws://192.168.110.223", pin = "a".repeat(64)))
    assertFalse(allow(loop, "pinned-link", source = "wss://link.example"))
    assertFalse(allow("ws://192.168.110.223/desktop", "pinned-link", source = "wss://link.example", pin = "a".repeat(64)))
  }
  @Test fun malformedCredentialsAndUnboundPathsAreRejected() {
    for (url in listOf("ws://user:secret@192.168.110.223/desktop", "ws://192.168.110.223/desktop?auth=x",
      "ws://192.168.110.223/desktop#x", "ws://192.168.110.223/a/../desktop", "ws://192.168.110.223/%64esktop",
      "ws://192.168.110.223:0/desktop", "file:///desktop")) assertFalse(url, allow(url, bound = "ws://192.168.110.223"))
    assertFalse(allow("ws://192.168.110.223/desktop", bound = "ws://192.168.110.223:9876"))
  }
  @Test fun ipv4MappedIpv6UsesTheEmbeddedAddressPolicy() {
    val prefix = ByteArray(12).also { it[10] = 255.toByte(); it[11] = 255.toByte() }
    assertTrue(DesktopTransportPolicy.privateAddress(prefix + byteArrayOf(192.toByte(), 168.toByte(), 1, 2)))
    assertFalse(DesktopTransportPolicy.privateAddress(prefix + byteArrayOf(8, 8, 8, 8)))
  }
}
