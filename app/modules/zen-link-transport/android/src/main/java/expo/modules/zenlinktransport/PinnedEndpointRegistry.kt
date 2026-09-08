package expo.modules.zenlinktransport

import java.util.concurrent.ConcurrentHashMap

// Native consumers use live listener ownership, never a JS assertion that an
// arbitrary loopback socket is encrypted. Every accepted stream still pins TLS.
object PinnedEndpointRegistry {
  private data class Entry(val owner: Any, val pin: String)
  private val entries = ConcurrentHashMap<Int, Entry>()

  internal fun register(port: Int, pin: String, owner: Any) {
    entries[port] = Entry(owner, pin.lowercase())
  }

  internal fun remove(port: Int, owner: Any) {
    entries.computeIfPresent(port) { _, entry -> if (entry.owner === owner) null else entry }
  }

  fun contains(port: Int, pin: String): Boolean = entries[port]?.pin == pin.lowercase()
}
