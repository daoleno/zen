package com.daoleno.zen

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.preference.PreferenceManager
import android.text.InputType
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView
import com.facebook.react.packagerconnection.PackagerConnectionSettings
import java.net.HttpURLConnection
import java.net.URI
import java.util.concurrent.Executors

/** Debug source set only: no JS or daemon connection is needed to select Metro. */
class MetroConnectActivity : Activity() {
  private val executor = Executors.newSingleThreadExecutor()
  private var request: HttpURLConnection? = null

  override fun onCreate(savedInstanceState: Bundle?) {
    super.onCreate(savedInstanceState)
    if (!BuildConfig.DEBUG || BuildConfig.ZEN_STANDALONE) {
      finish()
      return
    }
    val preferences = PreferenceManager.getDefaultSharedPreferences(applicationContext)
    val layout = LinearLayout(this).apply {
      orientation = LinearLayout.VERTICAL
      val padding = (24 * resources.displayMetrics.density).toInt()
      setPadding(padding, padding * 3, padding, padding)
    }
    layout.addView(TextView(this).apply { text = "Zen Development"; textSize = 24f })
    val address = EditText(this).apply {
      hint = "Metro host:port"
      contentDescription = "Metro host and port"
      inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI
      setSingleLine(true)
      setText(preferences.getString("debug_http_host", ""))
      id = View.generateViewId()
    }
    layout.addView(address)
    val status = TextView(this).apply { accessibilityLiveRegion = View.ACCESSIBILITY_LIVE_REGION_POLITE }
    val connect = Button(this).apply { text = "Connect" }
    layout.addView(connect)
    layout.addView(status)
    setContentView(layout)
    connect.setOnClickListener {
      val raw = address.text.toString().trim()
      val origin = try {
        val uri = URI(if (raw.contains("://")) raw else "http://$raw")
        require(uri.scheme == "http" && !uri.host.isNullOrBlank() && uri.port in 1..65535 &&
          uri.rawUserInfo == null && uri.rawQuery == null && uri.rawFragment == null &&
          (uri.rawPath.isNullOrEmpty() || uri.rawPath == "/"))
        uri
      } catch (_: Exception) {
        status.text = "Enter a Metro HTTP host and port."
        return@setOnClickListener
      }
      connect.isEnabled = false
      address.isEnabled = false
      status.text = "Connecting to ${origin.rawAuthority}..."
      executor.execute {
        val error = try {
          val connection = URI("http://${origin.rawAuthority}/status").toURL().openConnection() as HttpURLConnection
          request = connection
          connection.connectTimeout = 5000
          connection.readTimeout = 5000
          connection.instanceFollowRedirects = false
          try {
            check(connection.responseCode == 200)
            check(connection.inputStream.bufferedReader().use { it.readText() } == "packager-status:running")
          } finally {
            connection.disconnect()
            request = null
          }
          null
        } catch (_: Exception) {
          "Metro is unreachable at ${origin.rawAuthority}. Check the address and network, then retry."
        }
        runOnUiThread {
          if (isFinishing || isDestroyed) return@runOnUiThread
          connect.isEnabled = true
          address.isEnabled = true
          if (error != null) {
            status.text = error
            connect.text = "Retry"
          } else {
            preferences.edit().putString("debug_http_host", origin.rawAuthority).commit()
            val host = (application as MainApplication).reactHost
            PackagerConnectionSettings(applicationContext).debugServerHost = origin.rawAuthority
            if (host.currentReactContext != null) host.devSupportManager?.handleReloadJS()
            status.text = "Connected to ${origin.rawAuthority}"
            startActivity(Intent(this, MainActivity::class.java))
          }
        }
      }
    }
  }

  override fun onDestroy() {
    request?.disconnect()
    executor.shutdownNow()
    super.onDestroy()
  }
}
