package com.mcsekailink.app

import android.content.Context
import android.system.Os
import org.json.JSONArray
import java.io.BufferedInputStream
import java.io.File
import java.io.FileOutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.util.zip.ZipInputStream

object JreManager {

    fun jreDir(context: Context): File = File(context.filesDir, "jre")

    fun javaBin(context: Context): File = File(jreDir(context), "bin/java")

    fun isInstalled(context: Context): Boolean {
        val bin = javaBin(context)
        return bin.exists() && bin.canExecute()
    }

    /**
     * Fetches an Android Bionic-compatible ARM64 OpenJDK 21 package on demand.
     * Standard glibc Linux JREs do not run on Android Bionic, so we look for an
     * Android-arm64 zip artifact from community OpenJDK Android builds.
     */
    fun installArm64Jre(context: Context, onStatus: (String) -> Unit) {
        Thread {
            try {
                onStatus("Android ARM64 OpenJDK 21 surumu araniyor...")
                val downloadUrl = resolveAndroidJreZipUrl()
                    ?: throw IllegalStateException(
                        "Android Bionic ARM64 OpenJDK paketi su an otomatik alinamadi. " +
                            "Uzak PC/Linux modunu kullanabilir veya JRE paketini manuel yerlestirebilirsiniz."
                    )

                onStatus("Java 21 ARM64 indiriliyor (~90 MB)...")
                val tmpZip = File(context.cacheDir, "android-jre21.zip")
                downloadFile(downloadUrl, tmpZip)

                onStatus("Java 21 cikartiliyor...")
                val dest = jreDir(context)
                dest.deleteRecursively()
                dest.mkdirs()
                unzipSafe(tmpZip, dest)
                tmpZip.delete()

                // Normalize nested folder if archive contained a top-level dir
                if (!javaBin(context).exists()) {
                    val sub = dest.listFiles()?.firstOrNull { it.isDirectory && File(it, "bin/java").exists() }
                    if (sub != null) {
                        sub.listFiles()?.forEach { child ->
                            child.renameTo(File(dest, child.name))
                        }
                        sub.deleteRecursively()
                    }
                }

                markExecutables(File(dest, "bin"))
                if (isInstalled(context)) {
                    onStatus("Java 21 ARM64 hazir!")
                } else {
                    onStatus("Java kurulumu tamamlandi ancak calistirilamadi.")
                }
            } catch (e: Exception) {
                onStatus("Java kurulum hatasi: ${e.message}")
            }
        }.start()
    }

    private fun resolveAndroidJreZipUrl(): String? {
        return try {
            val conn = (URL("https://api.github.com/repos/PojavLauncherTeam/android-openjdk-build-multiarch/releases").openConnection() as HttpURLConnection).apply {
                setRequestProperty("User-Agent", "MC-SekaiLink-Android/0.1.0")
                connectTimeout = 10_000
                readTimeout = 15_000
            }
            if (conn.responseCode != 200) return null
            val body = conn.inputStream.bufferedReader().readText()
            val releases = JSONArray(body)
            for (i in 0 until releases.length()) {
                val rel = releases.getJSONObject(i)
                val assets = rel.optJSONArray("assets") ?: continue
                for (j in 0 until assets.length()) {
                    val asset = assets.getJSONObject(j)
                    val name = asset.optString("name").lowercase()
                    if (name.contains("jre21") && (name.contains("arm64") || name.contains("aarch64")) && name.endsWith(".zip")) {
                        return asset.optString("browser_download_url")
                    }
                }
            }
            null
        } catch (_: Exception) {
            null
        }
    }

    private fun downloadFile(urlStr: String, dest: File) {
        val conn = (URL(urlStr).openConnection() as HttpURLConnection).apply {
            setRequestProperty("User-Agent", "MC-SekaiLink-Android/0.1.0")
            instanceFollowRedirects = true
            connectTimeout = 15_000
            readTimeout = 60_000
        }
        conn.inputStream.use { input ->
            FileOutputStream(dest).use { out ->
                input.copyTo(out)
            }
        }
    }

    private fun unzipSafe(zipFile: File, targetDir: File) {
        val canonicalTarget = targetDir.canonicalPath + File.separator
        ZipInputStream(BufferedInputStream(zipFile.inputStream())).use { zis ->
            var entry = zis.nextEntry
            while (entry != null) {
                val outFile = File(targetDir, entry.name)
                if (!outFile.canonicalPath.startsWith(canonicalTarget)) {
                    throw SecurityException("Blocked zip-slip path: ${entry.name}")
                }
                if (entry.isDirectory) {
                    outFile.mkdirs()
                } else {
                    outFile.parentFile?.mkdirs()
                    FileOutputStream(outFile).use { fos ->
                        zis.copyTo(fos)
                    }
                }
                zis.closeEntry()
                entry = zis.nextEntry
            }
        }
    }

    private fun markExecutables(dir: File) {
        if (!dir.exists()) return
        dir.listFiles()?.forEach { f ->
            if (f.isFile) {
                try {
                    Os.chmod(f.absolutePath, 493) // 0755
                } catch (_: Exception) {
                    f.setExecutable(true, false)
                }
            }
        }
    }
}
