package expo.modules.zenremotedesktop

internal object DesktopFailure {
  fun map(code: Int?, body: String): Pair<String, String> {
    val text = body.trim()
    return when {
      code == 401 -> "denied" to "This device is no longer paired with the computer."
      text == "desktop_tls_required" ->
        "disconnected" to "Unattended desktop needs this computer's identity-bound encrypted transport. The unencrypted LAN switch is only for attended assistance."
      text == "desktop_scope_required" ->
        "denied" to "This phone has terminal access only. Run zen pair on the computer and scan once to grant unattended desktop."
      text == "host_setup_required" ->
        "unsupported" to "No current desktop session for this zen process. Start zen from the logged-in session, or run one OS-admin zen desktop-host --install for lock and login after reboot."
      code == 403 -> "disconnected" to text.ifBlank { "Desktop connection was refused." }
      else -> "disconnected" to "Desktop connection ended."
    }
  }
}
