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
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import java.io.File
import java.net.Inet4Address

class ServerForegroundService : Service() {

    companion object {
        const val ACTION_START = "com.mcsekailink.app.START"
        const val ACTION_STOP = "com.mcsekailink.app.STOP"
        private const val CHANNEL_ID = "sekailink_core_channel"
        private const val NOTIFICATION_ID = 1001
    }

    private var daemonProcess: Process? = null
    private var wakeLock: PowerManager.WakeLock? = null

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

    private fun acquireWakeLock() {
        if (wakeLock?.isHeld == true) return
        val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
        wakeLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "MCSekaiLink::ServerWakeLock").apply {
            acquire()
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
