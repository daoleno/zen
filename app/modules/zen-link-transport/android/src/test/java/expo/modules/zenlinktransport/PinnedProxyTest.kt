package expo.modules.zenlinktransport

import java.net.ServerSocket
import java.net.Socket
import java.net.SocketTimeoutException
import org.junit.Assert.assertEquals
import org.junit.Test

class PinnedProxyTest {
    @Test
    fun onDemandStartDoesNotOpenRemoteAdmissionConnection() {
        ServerSocket(0).use { remote ->
            remote.soTimeout = 250
            PinnedProxy(
                host = "127.0.0.1",
                port = remote.localPort,
                pin = "00".repeat(32),
                measureBeforeListen = false,
            ).use { proxy ->
                proxy.start()
                assertEquals(0, proxy.lastRttMs)
                try {
                    remote.accept().use {
                        throw AssertionError("on-demand start opened a remote preflight")
                    }
                } catch (_: SocketTimeoutException) {
                    // The listener exists, but no admission-bearing TLS stream
                    // starts until the real local /pair request connects.
                }

                remote.soTimeout = 1_000
                Socket("127.0.0.1", proxy.localPort).use {
                    remote.accept().use {
                        // A real local HTTP owner now causes the one remote
                        // stream on which pinned TLS and /pair will run.
                    }
                }
            }
        }
    }
}
