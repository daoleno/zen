package expo.modules.zenlinktransport

import java.net.ServerSocket
import java.net.Socket
import java.net.SocketTimeoutException
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.net.SocketException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class PinnedProxyTest {
    @Test
    fun pumpTreatsClosedSocketAsConnectionLocalAndReleasesOwner() {
        val source = object : Socket() {
            override fun getInputStream(): InputStream = object : InputStream() {
                override fun read(): Int = throw SocketException("Socket closed")
            }
        }
        val destination = object : Socket() {
            override fun getOutputStream() = ByteArrayOutputStream()
        }
        val done = CountDownLatch(1)
        pumpSocket(source, destination, done)
        assertTrue(done.await(100, TimeUnit.MILLISECONDS))
        assertTrue(source.isClosed)
        assertTrue(destination.isClosed)
    }

    @Test
    fun pumpPreservesBytesAndHalfCloseOnCleanEof() {
        val bytes = "owned response".toByteArray()
        val output = ByteArrayOutputStream()
        val source = object : Socket() {
            override fun getInputStream() = ByteArrayInputStream(bytes)
        }
        var halfClosed = false
        val destination = object : Socket() {
            override fun getOutputStream() = output
            override fun shutdownOutput() { halfClosed = true }
        }
        val done = CountDownLatch(1)
        pumpSocket(source, destination, done)
        assertEquals("owned response", output.toString("UTF-8"))
        assertTrue(halfClosed)
        assertTrue(done.await(100, TimeUnit.MILLISECONDS))
        assertTrue(!source.isClosed && !destination.isClosed)
    }

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
