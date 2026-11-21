package com.mcsekailink.app

import android.Manifest
import android.annotation.SuppressLint
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.webkit.JavascriptInterface
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import androidx.activity.OnBackPressedCallback
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import java.net.HttpURLConnection
import java.net.URL

class MainActivity : AppCompatActivity() {

    private lateinit var webView: WebView
    private var fileUploadCallback: ValueCallback<Array<Uri>>? = null

    private val fileChooserLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        val data = result.data
        val uris = WebChromeClient.FileChooserParams.parseResult(result.resultCode, data)
        fileUploadCallback?.onReceiveValue(uris)
        fileUploadCallback = null
    }

    private val notifPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { _ -> }

    private val jrePickerLauncher = registerForActivityResult(
        ActivityResultContracts.OpenDocument()
    ) { uri ->
        if (uri != null) {
            JreManager.importJreFromUri(this, uri) { msg ->
                runOnUiThread {
                    Toast.makeText(this@MainActivity, msg, Toast.LENGTH_SHORT).show()
                }
            }
        }
    }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        requestNotificationPermissionIfNeeded()
        startLocalCoreService()

        webView = WebView(this).apply {
            setBackgroundColor(0xFF070B14.toInt())
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.databaseEnabled = true
            settings.allowFileAccess = true
            addJavascriptInterface(AndroidJsBridge(), "SekaiAndroid")
            webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(view: WebView?, request: WebResourceRequest?): Boolean {
                    val uri = request?.url ?: return false
                    if (uri.host == "127.0.0.1" || uri.host == "localhost") {
                        return false
                    }
                    startActivity(Intent(Intent.ACTION_VIEW, uri))
                    return true
                }
            }
            webChromeClient = object : WebChromeClient() {
                override fun onShowFileChooser(
                    webView: WebView?,
                    filePathCallback: ValueCallback<Array<Uri>>?,
                    fileChooserParams: FileChooserParams?
                ): Boolean {
                    fileUploadCallback?.onReceiveValue(null)
                    fileUploadCallback = filePathCallback
                    return try {
                        val intent = fileChooserParams?.createIntent() ?: Intent(Intent.ACTION_GET_CONTENT).apply {
                            type = "*/*"
                            addCategory(Intent.CATEGORY_OPENABLE)
                        }
                        fileChooserLauncher.launch(intent)
                        true
                    } catch (_: Exception) {
                        fileUploadCallback = null
                        false
                    }
                }
            }
        }
        setContentView(webView)

        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (webView.canGoBack()) {
                    webView.goBack()
                } else {
                    finish()
                }
            }
        })

        showLoadingSplash()
        waitForCoreAndLoad()
    }

    private fun requestNotificationPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            val granted = ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED
            if (!granted) {
                notifPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
            }
        }
    }

    private fun startLocalCoreService() {
        val intent = Intent(this, ServerForegroundService::class.java).apply {
            action = ServerForegroundService.ACTION_START
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundService(intent)
        } else {
            startService(intent)
        }
    }

    private fun showLoadingSplash() {
        val html = """
            <html>
              <body style="margin:0;background:#070b14;color:#e2e8f0;font-family:sans-serif;display:flex;flex-direction:column;align-items:center;justify-content:center;height:100vh;text-align:center;padding:24px;">
                <h2 style="color:#34d399;margin-bottom:8px;">MC SekaiLink</h2>
                <p style="font-size:14px;color:#94a3b8;">Yerel sunucu cekirdegi baslatiliyor...</p>
              </body>
            </html>
        """.trimIndent()
        webView.loadDataWithBaseURL(null, html, "text/html", "utf-8", null)
    }

    private fun waitForCoreAndLoad() {
        Thread {
            var ready = false
            for (i in 1..25) {
                try {
                    val conn = (URL("http://127.0.0.1:25580/api/health").openConnection() as HttpURLConnection).apply {
                        connectTimeout = 500
                        readTimeout = 500
                    }
                    if (conn.responseCode == 200) {
                        ready = true
                        break
                    }
                } catch (_: Exception) {
                }
                Thread.sleep(250)
            }
            runOnUiThread {
                if (ready) {
                    webView.loadUrl("http://127.0.0.1:25580")
                } else {
                    webView.loadDataWithBaseURL(
                        null,
                        """
                        <html><body style="background:#070b14;color:#f8fafc;font-family:sans-serif;padding:24px;text-align:center;">
                          <h2 style="color:#fb7185;">Yerel Cekirdek Baslatilamadi</h2>
                          <p style="color:#94a3b8;font-size:14px;">libsekailink_core.so bulunamadi veya baslatilamadi. Tekrar deneyebilir veya uzak sunucuya baglanabilirsiniz.</p>
                        </body></html>
                        """.trimIndent(),
                        "text/html",
                        "utf-8",
                        null
                    )
                }
            }
        }.start()
    }

    inner class AndroidJsBridge {
        @JavascriptInterface
        fun getPlatform(): String = "android"

        @JavascriptInterface
        fun isJreInstalled(): Boolean = JreManager.isInstalled(this@MainActivity)

        @JavascriptInterface
        fun installArm64Jre() {
            JreManager.installArm64Jre(this@MainActivity) { msg ->
                runOnUiThread {
                    Toast.makeText(this@MainActivity, msg, Toast.LENGTH_SHORT).show()
                }
            }
        }

        @JavascriptInterface
        fun importArm64JreFile() {
            runOnUiThread {
                jrePickerLauncher.launch(arrayOf("*/*"))
            }
        }

        @JavascriptInterface
        fun stopLocalService() {
            val stopIntent = Intent(this@MainActivity, ServerForegroundService::class.java).apply {
                action = ServerForegroundService.ACTION_STOP
            }
            startService(stopIntent)
        }

        @JavascriptInterface
        fun requestBatteryUnrestricted() {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                val intent = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).apply {
                    data = Uri.parse("package:$packageName")
                }
                startActivity(intent)
            }
        }
    }
}
