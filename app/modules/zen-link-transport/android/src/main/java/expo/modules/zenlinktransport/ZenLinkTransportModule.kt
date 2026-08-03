package expo.modules.zenlinktransport

import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import java.io.Closeable
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.ServerSocket
import java.net.Socket
import java.security.MessageDigest
import java.security.SecureRandom
import java.security.cert.X509Certificate
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.Semaphore
import java.util.concurrent.atomic.AtomicBoolean
import javax.net.ssl.SNIHostName
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocket
import javax.net.ssl.X509TrustManager

class ZenLinkTransportModule : Module() {
    private val proxies = ConcurrentHashMap<String, PinnedProxy>()

    override fun definition() = ModuleDefinition {
        Name("ZenLinkTransport")

        AsyncFunction("start") { key: String, host: String, port: Int, pin: String, mode: String ->
            validateInput(key, host, port, pin, mode)
            proxies[key]?.let { existing ->
                if (existing.matches(host, port, pin, mode)) {
                    return@AsyncFunction mapOf(
                        "port" to existing.localPort,
                        "rttMs" to existing.lastRttMs,
                    )
                }
                existing.close()
                proxies.remove(key, existing)
            }
            val proxy = PinnedProxy(host, port, pin, mode == "measure")
            try {
                proxy.start()
                proxies[key] = proxy
                mapOf("port" to proxy.localPort, "rttMs" to proxy.lastRttMs)
            } catch (error: Throwable) {
                proxy.close()
                throw IllegalStateException("Zen Link could not reach a pinned relay candidate.", error)
            }
        }

        AsyncFunction("stop") { key: String ->
            proxies.remove(key)?.close()
        }

        AsyncFunction("stopAll") {
            stopAll()
        }

        OnDestroy {
            stopAll()
        }
    }

    private fun stopAll() {
        val current = proxies.values.toList()
        proxies.clear()
        current.forEach { it.close() }
    }

    private fun validateInput(key: String, host: String, port: Int, pin: String, mode: String) {
        require(key.isNotBlank()) { "Zen Link tunnel key is required." }
        require(host.isNotBlank() && !host.contains('/') && !host.contains('\u0000')) {
            "Zen Link host is invalid."
        }
        require(port in 1..65535) { "Zen Link port is invalid." }
        require(pin.matches(Regex("^[0-9a-fA-F]{64}$"))) {
            "Zen Link SPKI pin is invalid."
        }
        require(mode == "measure" || mode == "on-demand") {
            "Zen Link tunnel mode is invalid."
        }
    }
}

internal class PinnedProxy(
    private val host: String,
    private val port: Int,
    private val pin: String,
    private val measureBeforeListen: Boolean,
) : Closeable {
    private val running = AtomicBoolean(false)
    private val worker = Executors.newCachedThreadPool { task ->
        Thread(task, "zen-link-transport").apply { isDaemon = true }
    }
    private val connectionLimit = Semaphore(64)
    private val openSockets = ConcurrentHashMap.newKeySet<Socket>()
    private val sslContext = pinnedSSLContext(pin)
    private lateinit var listener: ServerSocket

    var localPort: Int = 0
        private set
    var lastRttMs: Int = 0
        private set

    fun matches(
        otherHost: String,
        otherPort: Int,
        otherPin: String,
        mode: String,
    ): Boolean =
        host == otherHost &&
            port == otherPort &&
            pin.equals(otherPin, ignoreCase = true) &&
            measureBeforeListen == (mode == "measure") &&
            running.get()

    fun start() {
        if (measureBeforeListen) {
            val startedAt = System.nanoTime()
            createRemoteSocket().use { remote ->
                remote.startHandshake()
            }
            lastRttMs = maxOf(1, ((System.nanoTime() - startedAt) / 1_000_000L).toInt())
        }

        listener = ServerSocket()
        listener.reuseAddress = true
        listener.bind(InetSocketAddress(InetAddress.getByName("127.0.0.1"), 0), 64)
        localPort = listener.localPort
        running.set(true)
        worker.execute { acceptLoop() }
    }

    private fun acceptLoop() {
        while (running.get()) {
            val local = try {
                listener.accept()
            } catch (_: Throwable) {
                break
            }
            if (!connectionLimit.tryAcquire()) {
                local.close()
                continue
            }
            openSockets.add(local)
            worker.execute {
                try {
                    bridge(local)
                } finally {
                    openSockets.remove(local)
                    closeQuietly(local)
                    connectionLimit.release()
                }
            }
        }
    }

    private fun bridge(local: Socket) {
        val remote = createRemoteSocket()
        openSockets.add(remote)
        try {
            remote.startHandshake()
            remote.soTimeout = 0
            val done = CountDownLatch(2)
            worker.execute {
                try {
                    local.getInputStream().copyTo(remote.getOutputStream(), 32 * 1024)
                    runCatching { remote.shutdownOutput() }
                } finally {
                    done.countDown()
                }
            }
            worker.execute {
                try {
                    remote.getInputStream().copyTo(local.getOutputStream(), 32 * 1024)
                    runCatching { local.shutdownOutput() }
                } finally {
                    done.countDown()
                }
            }
            done.await()
        } finally {
            openSockets.remove(remote)
            closeQuietly(remote)
        }
    }

    private fun createRemoteSocket(): SSLSocket {
        val plain = Socket()
        plain.tcpNoDelay = true
        plain.keepAlive = true
        plain.connect(InetSocketAddress(host, port), 5_000)
        val socket = sslContext.socketFactory.createSocket(plain, host, port, true) as SSLSocket
        socket.enabledProtocols = arrayOf("TLSv1.3")
        val parameters = socket.sslParameters
        parameters.serverNames = listOf(SNIHostName(host))
        socket.sslParameters = parameters
        socket.soTimeout = 15_000
        return socket
    }

    override fun close() {
        if (!running.getAndSet(false) && !::listener.isInitialized) {
            worker.shutdownNow()
            return
        }
        if (::listener.isInitialized) {
            closeQuietly(listener)
        }
        openSockets.forEach(::closeQuietly)
        openSockets.clear()
        worker.shutdownNow()
    }
}

private fun pinnedSSLContext(pinHex: String): SSLContext {
    val expected = pinHex.lowercase().chunked(2)
        .map { it.toInt(16).toByte() }
        .toByteArray()
    val trustManager = object : X509TrustManager {
        override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()

        override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) {
            throw java.security.cert.CertificateException("Client certificates are unsupported.")
        }

        override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
            val leaf = chain.firstOrNull()
                ?: throw java.security.cert.CertificateException("Zen Link peer sent no certificate.")
            val actual = MessageDigest.getInstance("SHA-256").digest(leaf.publicKey.encoded)
            if (!MessageDigest.isEqual(actual, expected)) {
                throw java.security.cert.CertificateException("Zen Link transport certificate pin mismatch.")
            }
        }
    }
    return SSLContext.getInstance("TLSv1.3").apply {
        init(null, arrayOf(trustManager), SecureRandom())
    }
}

private fun closeQuietly(closeable: Closeable) {
    runCatching { closeable.close() }
}
