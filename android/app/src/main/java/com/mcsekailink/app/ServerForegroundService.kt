package com.mcsekailink.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.wifi.WifiManager
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import android.util.Base64
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import java.io.File
import java.net.Inet4Address
import java.security.KeyStore
import java.security.cert.X509Certificate
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.X509TrustManager

class ServerForegroundService : Service() {

    companion object {
        const val ACTION_START = "com.mcsekailink.app.START"
        const val ACTION_STOP = "com.mcsekailink.app.STOP"
        private const val CHANNEL_ID = "sekailink_core_channel"
        private const val NOTIFICATION_ID = 1001
    }

    private var daemonProcess: Process? = null
    private var wakeLock: PowerManager.WakeLock? = null
    private var wifiLock: WifiManager.WifiLock? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopDaemon()
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
            return START_NOT_STICKY
        }

        ServiceCompat.startForeground(
            this,
            NOTIFICATION_ID,
            buildNotification(),
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC else 0
        )
        acquireWakeLock()
        startDaemonIfNeeded()
        return START_STICKY
    }

    override fun onDestroy() {
        stopDaemon()
        releaseWakeLock()
        super.onDestroy()
    }

    private fun startDaemonIfNeeded() {
        if (daemonProcess != null && isAlive(daemonProcess)) {
            return
        }
        val nativeDir = applicationInfo.nativeLibraryDir
        val bin = File(nativeDir, "libsekailink_core.so")
        if (!bin.exists()) {
            return
        }

        val dataDir = File(filesDir, "sekailink").apply { mkdirs() }
        val tmpDir = File(cacheDir, "tmp").apply { mkdirs() }
        val logFile = File(cacheDir, "core.log")
        val jreDir = File(filesDir, "jre")

        val cmd = listOf(
            bin.absolutePath,
            "--port", "25580",
            "--host", "127.0.0.1",
            "--data-dir", dataDir.absolutePath
        )

        val pb = ProcessBuilder(cmd)
        pb.directory(dataDir)
        pb.redirectErrorStream(true)
        pb.redirectOutput(ProcessBuilder.Redirect.to(logFile))
        val env = pb.environment()
        env["SEKAILINK_PLATFORM"] = "android"
        env["SEKAILINK_DATA_DIR"] = dataDir.absolutePath
        env["SEKAILINK_NATIVE_LIB_DIR"] = nativeDir
        env["SEKAILINK_JAVA_HOME"] = jreDir.absolutePath
        env["SEKAILINK_DNS_SERVERS"] = detectDnsServers()
        env["QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING"] = "true"
        env["HOME"] = filesDir.absolutePath
        env["TMPDIR"] = tmpDir.absolutePath

        val caBundle = exportAndroidCaBundle(File(cacheDir, "cacerts.pem"))
        if (caBundle != null) {
            env["SSL_CERT_FILE"] = caBundle.absolutePath
        }
        val certDirs = detectAndroidCertDirs()
        if (certDirs.isNotEmpty()) {
            env["SSL_CERT_DIR"] = certDirs
        }

        try {
            daemonProcess = pb.start()
        } catch (_: Exception) {
            daemonProcess = null
        }
    }

    private fun stopDaemon() {
        val p = daemonProcess ?: return
        daemonProcess = null
        try {
            p.destroy()
        } catch (_: Exception) {
        }
    }

    private fun isAlive(p: Process?): Boolean {
        if (p == null) return false
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            p.isAlive
        } else {
            try {
                p.exitValue()
                false
            } catch (_: IllegalThreadStateException) {
                true
            }
        }
    }

    private fun detectDnsServers(): String {
        return try {
            val cm = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
            val network = cm.activeNetwork ?: return "1.1.1.1,8.8.8.8"
            val lp = cm.getLinkProperties(network) ?: return "1.1.1.1,8.8.8.8"
            val ips = lp.dnsServers
                .filterIsInstance<Inet4Address>()
                .mapNotNull { it.hostAddress }
            if (ips.isEmpty()) "1.1.1.1,8.8.8.8" else ips.joinToString(",")
        } catch (_: Exception) {
            "1.1.1.1,8.8.8.8"
        }
    }

    private fun exportAndroidCaBundle(destFile: File): File? {
        return try {
            val certs = linkedSetOf<X509Certificate>()
            val tmf = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm()).apply {
                init(null as KeyStore?)
            }
            for (tm in tmf.trustManagers) {
                if (tm is X509TrustManager) {
                    tm.acceptedIssuers?.forEach { certs.add(it) }
                }
            }
            if (certs.isEmpty()) {
                val ks = KeyStore.getInstance("AndroidCAStore").apply { load(null) }
                val aliases = ks.aliases()
                while (aliases.hasMoreElements()) {
                    val cert = ks.getCertificate(aliases.nextElement())
                    if (cert is X509Certificate) {
                        certs.add(cert)
                    }
                }
            }
            if (certs.isEmpty()) {
                return null
            }

            val tmpFile = File(destFile.parentFile, "${destFile.name}.tmp")
            tmpFile.bufferedWriter(Charsets.UTF_8).use { writer ->
                for (cert in certs) {
                    val b64 = Base64.encodeToString(cert.encoded, Base64.DEFAULT).trim()
                    writer.append("-----BEGIN CERTIFICATE-----\n")
                    writer.append(b64)
                    writer.append("\n-----END CERTIFICATE-----\n")
                }
            }
            if (!tmpFile.renameTo(destFile)) {
                tmpFile.copyTo(destFile, overwrite = true)
                tmpFile.delete()
            }
            if (destFile.exists() && destFile.length() > 0L) destFile else null
        } catch (_: Exception) {
            null
        }
    }

    private fun detectAndroidCertDirs(): String {
        val candidates = listOf(
            "/apex/com.android.conscrypt/cacerts",
            "/system/etc/security/cacerts",
            "/data/misc/user/0/cacerts-added",
            "/data/misc/keychain/certs-added"
        )
        return candidates.filter { path ->
            val dir = File(path)
            dir.isDirectory && !dir.list().isNullOrEmpty()
        }.joinToString(":")
    }

    private fun acquireWakeLock() {
        if (wakeLock?.isHeld != true) {
            val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
            wakeLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "MCSekaiLink::ServerWakeLock").apply {
                acquire()
            }
        }
        if (wifiLock?.isHeld != true) {
            try {
                val wm = applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
                if (wm != null) {
                    val mode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                        WifiManager.WIFI_MODE_FULL_LOW_LATENCY
                    } else {
                        @Suppress("DEPRECATION")
                        WifiManager.WIFI_MODE_FULL_HIGH_PERF
                    }
                    wifiLock = wm.createWifiLock(mode, "MCSekaiLink::ServerWifiLock").apply {
                        setReferenceCounted(false)
                        acquire()
                    }
                }
            } catch (_: Exception) {
            }
        }
    }

    private fun releaseWakeLock() {
        try {
            if (wakeLock?.isHeld == true) {
                wakeLock?.release()
            }
        } catch (_: Exception) {
        }
        wakeLock = null

        try {
            if (wifiLock?.isHeld == true) {
                wifiLock?.release()
            }
        } catch (_: Exception) {
        }
        wifiLock = null
    }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                "MC SekaiLink Sunucu Servisi",
                NotificationManager.IMPORTANCE_LOW
            )
            val nm = getSystemService(NotificationManager::class.java)
            nm.createNotificationChannel(channel)
        }
    }

    private fun buildNotification(): Notification {
        val openIntent = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE
        )
        val stopIntent = PendingIntent.getService(
            this,
            1,
            Intent(this, ServerForegroundService::class.java).apply { action = ACTION_STOP },
            PendingIntent.FLAG_IMMUTABLE
        )

        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle(getString(R.string.service_notification_title))
            .setContentText(getString(R.string.service_notification_text))
            .setSmallIcon(android.R.drawable.ic_media_play)
            .setContentIntent(openIntent)
            .setOngoing(true)
            .addAction(android.R.drawable.ic_menu_close_clear_cancel, getString(R.string.action_stop), stopIntent)
            .build()
    }
}
