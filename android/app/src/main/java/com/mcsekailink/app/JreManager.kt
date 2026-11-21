package com.mcsekailink.app

import android.content.Context
import android.net.Uri
import android.provider.OpenableColumns
import android.system.Os
import org.apache.commons.compress.archivers.tar.TarArchiveEntry
import org.apache.commons.compress.archivers.tar.TarArchiveInputStream
import org.apache.commons.compress.compressors.gzip.GzipCompressorInputStream
import org.apache.commons.compress.compressors.xz.XZCompressorInputStream
import org.json.JSONArray
import java.io.BufferedInputStream
import java.io.File
import java.io.FileOutputStream
import java.io.InputStream
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
     * Downloads an Android Bionic-compatible ARM64 OpenJDK runtime (.tar.xz / .tar.gz / .zip)
     * from PojavLauncher's multiarch OpenJDK releases on demand.
     */
    fun installArm64Jre(context: Context, onStatus: (String) -> Unit) {
        Thread {
            try {
                onStatus("Android ARM64 OpenJDK paketi araniyor...")
                val asset = resolveAndroidJreAsset()
                    ?: throw IllegalStateException(
                        "Android ARM64 OpenJDK paketi otomatik bulunamadi. " +
                            "Telefondan manuel .tar.xz/.zip JRE secebilir veya Uzak PC/Linux modunu kullanabilirsiniz."
                    )

                val (assetName, downloadUrl) = asset
                onStatus("$assetName indiriliyor...")
                val tmpFile = File(context.cacheDir, assetName)
                downloadFile(downloadUrl, tmpFile)

                installFromArchiveFile(context, tmpFile, assetName, onStatus)
                tmpFile.delete()
            } catch (e: Exception) {
                onStatus("Java kurulum hatasi: ${e.message}")
            }
        }.start()
    }

    /**
     * Installs an ARM64 OpenJDK runtime from a user-selected .tar.xz, .tar.gz, or .zip document URI.
     */
    fun importJreFromUri(context: Context, uri: Uri, onStatus: (String) -> Unit) {
        Thread {
            var tmpFile: File? = null
            try {
                val displayName = resolveDisplayName(context, uri) ?: "custom-jre.tar.xz"
                onStatus("$displayName kopyalaniyor...")
                tmpFile = File(context.cacheDir, "import_${System.currentTimeMillis()}_$displayName")
                context.contentResolver.openInputStream(uri)?.use { input ->
                    FileOutputStream(tmpFile).use { output ->
                        input.copyTo(output)
                    }
                } ?: throw IllegalStateException("Secilen dosya okunamadi")

                installFromArchiveFile(context, tmpFile, displayName, onStatus)
            } catch (e: Exception) {
                onStatus("JRE ice aktarma hatasi: ${e.message}")
            } finally {
                tmpFile?.delete()
            }
        }.start()
    }

    private fun installFromArchiveFile(
        context: Context,
        archiveFile: File,
        nameHint: String,
        onStatus: (String) -> Unit
    ) {
        onStatus("Java calisma ortami cikartiliyor...")
        val dest = jreDir(context)
        dest.deleteRecursively()
        dest.mkdirs()

        extractArchiveSafe(archiveFile, nameHint, dest)

        // Normalize nested top-level folder if archive extracted into a single subdirectory
        if (!javaBin(context).exists()) {
            val sub = dest.listFiles()?.firstOrNull { it.isDirectory && File(it, "bin/java").exists() }
            if (sub != null) {
                sub.listFiles()?.forEach { child ->
                    child.renameTo(File(dest, child.name))
                }
                sub.deleteRecursively()
            }
        }

        markExecutablesRecursive(File(dest, "bin"))
        markSharedLibsRecursive(File(dest, "lib"))
        if (isInstalled(context)) {
            onStatus("Android ARM64 Java hazir!")
        } else {
            onStatus("Arsiv cikartildi ancak bin/java bulunamadi.")
        }
    }

    private fun resolveAndroidJreAsset(): Pair<String, String>? {
        return try {
            val conn = (URL("https://api.github.com/repos/PojavLauncherTeam/android-openjdk-build-multiarch/releases?per_page=15").openConnection() as HttpURLConnection).apply {
                setRequestProperty("User-Agent", "MC-SekaiLink-Android/0.1.0")
                connectTimeout = 10_000
                readTimeout = 15_000
            }
            if (conn.responseCode != 200) return null
            val body = conn.inputStream.bufferedReader().readText()
            val releases = JSONArray(body)

            for (preferredTag in listOf("jre25", "jre21", "jre17")) {
                for (i in 0 until releases.length()) {
                    val rel = releases.getJSONObject(i)
                    val assets = rel.optJSONArray("assets") ?: continue
                    for (j in 0 until assets.length()) {
                        val asset = assets.getJSONObject(j)
                        val name = asset.optString("name").lowercase()
                        val matchesArch = name.contains("arm64") || name.contains("aarch64")
                        val matchesExt = name.endsWith(".tar.xz") || name.endsWith(".tar.gz") || name.endsWith(".tgz") || name.endsWith(".zip")
                        if (name.contains(preferredTag) && matchesArch && matchesExt) {
                            return Pair(asset.optString("name"), asset.optString("browser_download_url"))
                        }
                    }
                }
            }
            null
        } catch (_: Exception) {
            null
        }
    }

    private fun extractArchiveSafe(archiveFile: File, nameHint: String, targetDir: File) {
        val lower = nameHint.lowercase()
        when {
            lower.endsWith(".zip") -> unzipSafe(archiveFile, targetDir)
            lower.endsWith(".tar.xz") || lower.endsWith(".txz") -> {
                BufferedInputStream(archiveFile.inputStream()).use { bis ->
                    XZCompressorInputStream(bis).use { xzis ->
                        untarSafe(xzis, targetDir)
                    }
                }
            }
            lower.endsWith(".tar.gz") || lower.endsWith(".tgz") -> {
                BufferedInputStream(archiveFile.inputStream()).use { bis ->
                    GzipCompressorInputStream(bis).use { gzis ->
                        untarSafe(gzis, targetDir)
                    }
                }
            }
            else -> throw IllegalArgumentException("Desteklenmeyen arsiv turu: $nameHint (.tar.xz, .tar.gz veya .zip secin)")
        }
    }

    private fun untarSafe(rawStream: InputStream, targetDir: File) {
        val canonicalTarget = targetDir.canonicalPath + File.separator
        TarArchiveInputStream(rawStream).use { tis ->
            var entry: TarArchiveEntry? = tis.nextEntry
            while (entry != null) {
                val outFile = File(targetDir, entry.name)
                if (!outFile.canonicalPath.startsWith(canonicalTarget) && outFile.canonicalPath != targetDir.canonicalPath) {
                    throw SecurityException("Blocked tar-slip path: ${entry.name}")
                }
                when {
                    entry.isDirectory -> outFile.mkdirs()
                    entry.isSymbolicLink -> {
                        outFile.parentFile?.mkdirs()
                        if (outFile.exists()) outFile.delete()
                        try {
                            Os.symlink(entry.linkName, outFile.absolutePath)
                        } catch (_: Exception) {
                        }
                    }
                    entry.isFile -> {
                        outFile.parentFile?.mkdirs()
                        FileOutputStream(outFile).use { fos ->
                            tis.copyTo(fos)
                        }
                        if ((entry.mode and 0b001_000_000) != 0) {
                            try {
                                Os.chmod(outFile.absolutePath, 493) // 0755
                            } catch (_: Exception) {
                                outFile.setExecutable(true, false)
                            }
                        }
                    }
                }
                entry = tis.nextEntry
            }
        }
    }

    private fun unzipSafe(zipFile: File, targetDir: File) {
        val canonicalTarget = targetDir.canonicalPath + File.separator
        ZipInputStream(BufferedInputStream(zipFile.inputStream())).use { zis ->
            var entry = zis.nextEntry
            while (entry != null) {
                val outFile = File(targetDir, entry.name)
                if (!outFile.canonicalPath.startsWith(canonicalTarget) && outFile.canonicalPath != targetDir.canonicalPath) {
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

    private fun markExecutablesRecursive(dir: File) {
        if (!dir.exists()) return
        dir.listFiles()?.forEach { f ->
            if (f.isDirectory) {
                markExecutablesRecursive(f)
            } else {
                try {
                    Os.chmod(f.absolutePath, 493) // 0755
                } catch (_: Exception) {
                    f.setExecutable(true, false)
                }
            }
        }
    }

    private fun markSharedLibsRecursive(dir: File) {
        if (!dir.exists()) return
        dir.listFiles()?.forEach { f ->
            if (f.isDirectory) {
                markSharedLibsRecursive(f)
            } else if (f.name.endsWith(".so") || f.name.contains(".so.")) {
                try {
                    Os.chmod(f.absolutePath, 493) // 0755
                } catch (_: Exception) {
                    f.setExecutable(true, false)
                }
            }
        }
    }

    private fun resolveDisplayName(context: Context, uri: Uri): String? {
        return try {
            context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor ->
                if (cursor.moveToFirst()) cursor.getString(0) else null
            }
        } catch (_: Exception) {
            uri.lastPathSegment
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
}
