package expo.modules.zenremotedesktop

import java.net.URI
import okhttp3.HttpUrl
import okhttp3.Request

internal object DesktopTransportPolicy {
  fun privateAddress(bytes: ByteArray): Boolean {
    val b = bytes.map { it.toInt() and 255 }
    if (b.size == 16 && b.take(10).all { it == 0 } && b[10] == 255 && b[11] == 255) {
      return privateAddress(bytes.copyOfRange(12, 16))
    }
    return if (b.size == 4) b[0] == 10 || (b[0] == 172 && b[1] in 16..31) ||
      (b[0] == 192 && b[1] == 168) || (b[0] == 100 && b[1] in 64..127)
    else b.size == 16 && (b[0] and 254) == 252
  }

  private fun parse(value: String, endpoint: Boolean): HttpUrl? = try {
    val uri = URI(value)
    if (uri.scheme !in listOf("ws", "wss") || uri.rawUserInfo != null || uri.rawQuery != null || uri.rawFragment != null ||
        (if (endpoint) uri.rawPath != "/desktop" else uri.rawPath !in listOf("", "/"))) null
    else Request.Builder().url(value).build().url
  } catch (_: Exception) { null }

  private fun sameOrigin(a: HttpUrl, b: HttpUrl) = a.scheme == b.scheme && a.host == b.host && a.port == b.port

  fun allows(url: String, mode: String, boundOrigin: String, sourceOrigin: String, pin: String,
             numericAddress: (String) -> ByteArray?): Boolean {
    val target = parse(url, true) ?: return false
    val bound = parse(boundOrigin, false) ?: return false
    val source = parse(sourceOrigin, false) ?: return false
    if (!sameOrigin(target, bound)) return false
    return when (mode) {
      "tls" -> target.isHttps && source.isHttps && sameOrigin(target, source)
      "pinned-link" -> !target.isHttps && target.host == "127.0.0.1" && source.isHttps &&
        pin.length == 64 && pin.all { it in '0'..'9' || it in 'a'..'f' || it in 'A'..'F' }
      "trusted-lan" -> !target.isHttps && sameOrigin(target, source) &&
        !target.host.contains('%') && numericAddress(target.host)?.let(::privateAddress) == true
      else -> false
    }
  }
}
