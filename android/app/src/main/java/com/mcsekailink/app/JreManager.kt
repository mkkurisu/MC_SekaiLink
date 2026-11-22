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
import java.io.IOException
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.URL
import java.util.zip.ZipInputStream

object JreManager {

    private data class JreCandidate(
        val assetName: String,
        val downloadUrl: String
    )

    private val DIRECT_ANDROID_ARM64_CANDIDATES = listOf(
        JreCandidate(
            assetName = "jre25-android-arm64.tar.xz",
            downloadUrl = "https://github.com/AngelAuraMC/angelauramc-openjdk-build/releases/download/download_jre25/jre25-android-arm64.tar.xz"
        ),
        JreCandidate(
            assetName = "jre21-android-arm64.tar.xz",
            downloadUrl = "https://github.com/AngelAuraMC/angelauramc-openjdk-build/releases/download/download_jre21/jre21-android-arm64.tar.xz"
        )
    )

    fun jreDir(context: Context): File = File(context.filesDir, "jre")

    fun javaBin(context: Context): File = File(jreDir(context), "bin/java")

    fun isInstalled(context: Context): Boolean {
        val bin = javaBin(context)
        return bin.exists() && bin.canExecute() && isAndroidArm64Elf(bin)
    }

    /**
     * Downloads an Android Bionic-compatible ARM64 OpenJDK 25/21 runtime (.tar.xz)
     * verified to execute on Android ARM64 hardware.
     */
    fun installArm64Jre(context: Context, onStatus: (message: String, done: Boolean, installed: Boolean) -> Unit) {
        Thread {
            try {
                onStatus("Android ARM64 OpenJDK 25 paketi indiriliyor...", false, false)

                val candidates = LinkedHashMap<String, JreCandidate>()
                for (c in DIRECT_ANDROID_ARM64_CANDIDATES) {
                    candidates[c.assetName] = c
                }
                resolveAndroidJreAsset()?.let { fallback ->
                    candidates.putIfAbsent(fallback.assetName, fallback)
                }

                var lastErr: Exception? = null
                var succeeded = false
                for ((_, candidate) in candidates) {
                    val tmpFile = File(context.cacheDir, candidate.assetName)
                    try {
                        onStatus("${candidate.assetName} indiriliyor...", false, false)
                        downloadFile(candidate.downloadUrl, tmpFile)
                        installFromArchiveFile(context, tmpFile, candidate.assetName, onStatus)
                        succeeded = true
                        break
                    } catch (e: Exception) {
                        lastErr = e
                    } finally {
                        tmpFile.delete()
                    }
                }

                if (!succeeded) {
                    throw (lastErr ?: IllegalStateException("Android ARM64 OpenJDK paketi indirilemedi"))
                }
            } catch (e: Exception) {
                onStatus("Java kurulum hatasi: ${e.message ?: "Bilinmeyen hata"}", true, false)
            }
        }.start()
    }

    /**
     * Installs an ARM64 OpenJDK runtime from a user-selected .tar.xz, .tar.gz, or .zip document URI.
     */
    fun importJreFromUri(context: Context, uri: Uri, onStatus: (message: String, done: Boolean, installed: Boolean) -> Unit) {
        Thread {
            var tmpFile: File? = null
            try {
                val displayName = resolveDisplayName(context, uri) ?: "custom-jre.tar.xz"
                onStatus("$displayName kopyalaniyor...", false, false)
                tmpFile = File(context.cacheDir, "import_${System.currentTimeMillis()}_$displayName")
                context.contentResolver.openInputStream(uri)?.use { input ->
                    FileOutputStream(tmpFile).use { output ->
                        input.copyTo(output)
                    }
                } ?: throw IllegalStateException("Secilen dosya okunamadi")

                installFromArchiveFile(context, tmpFile, displayName, onStatus)
            } catch (e: Exception) {
                onStatus("JRE ice aktarma hatasi: ${e.message ?: "Bilinmeyen hata"}", true, false)
            } finally {
                tmpFile?.delete()
            }
        }.start()
    }

    private fun installFromArchiveFile(
        context: Context,
        archiveFile: File,
        nameHint: String,
        onStatus: (message: String, done: Boolean, installed: Boolean) -> Unit
    ) {
        onStatus("Java calisma ortami cikartiliyor...", false, false)
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

        val bin = javaBin(context)
        if (!bin.exists()) {
            dest.deleteRecursively()
            throw IllegalStateException("Arsiv cikartildi ancak bin/java bulunamadi.")
        }

        markExecutablesRecursive(File(dest, "bin"))
        markSharedLibsRecursive(File(dest, "lib"))
        val jspawnHelper = File(dest, "lib/jspawnhelper")
        if (jspawnHelper.exists()) {
            makeFileExecutable(jspawnHelper)
        }

        if (!isAndroidArm64Elf(bin)) {
            dest.deleteRecursively()
            throw IllegalStateException("Secilen arsiv Android ARM64 ELF Java dosyasi icermiyor (iOS/x86 uyumsuz arsiv).")
        }

        onStatus("Java calisma testi yapiliyor...", false, false)
        val probe = probeJavaVersion(context)
        probe.fold(
            onSuccess = { version ->
                onStatus("Android ARM64 Java hazir! ($version)", true, true)
            },
            onFailure = { err ->
                onStatus("Java calistirma testi basarisiz: ${err.message}", true, false)
            }
        )
    }

    private fun isAndroidArm64Elf(bin: File): Boolean {
        if (!bin.exists() || bin.length() < 20) return false
        return try {
            val header = ByteArray(20)
            bin.inputStream().use { input ->
                val read = input.read(header)
                if (read < 20) return false
            }
            // ELF magic: 0x7F 'E' 'L' 'F', EI_CLASS = 2 (ELF64), e_machine = 0x00B7 (EM_AARCH64 LE)
            header[0] == 0x7F.toByte() &&
                header[1] == 'E'.code.toByte() &&
                header[2] == 'L'.code.toByte() &&
                header[3] == 'F'.code.toByte() &&
                header[4] == 2.toByte() &&
                header[18] == 0xB7.toByte() &&
                header[19] == 0x00.toByte()
        } catch (_: Exception) {
            false
        }
    }

    private fun probeJavaVersion(context: Context): Result<String> {
        val jre = jreDir(context)
        val bin = javaBin(context)
        if (!bin.exists()) {
            return Result.failure(IllegalStateException("bin/java bulunamadi"))
        }
        return try {
            val pb = ProcessBuilder(bin.absolutePath, "-version").apply {
                directory(jre)
                redirectErrorStream(true)
            }
            val env = pb.environment()
            val libPaths = listOf(
                File(jre, "lib").absolutePath,
                File(jre, "lib/jli").absolutePath,
                File(jre, "lib/server").absolutePath
            ).joinToString(":")
            val existingLd = env["LD_LIBRARY_PATH"].orEmpty()
            env["JAVA_HOME"] = jre.absolutePath
            env["LD_LIBRARY_PATH"] = if (existingLd.isNotEmpty()) "$libPaths:$existingLd" else libPaths

            val proc = pb.start()
            val output = proc.inputStream.bufferedReader().readText().trim()
            val code = proc.waitFor()
            if (code == 0) {
                val ver = Regex("""version "([^"]+)"""").find(output)?.groupValues?.getOrNull(1)
                    ?: output.lineSequence().firstOrNull()?.trim()
                    ?: "Java"
                Result.success(ver)
            } else {
                val snippet = output.replace("\n", " ").take(180)
                Result.failure(IllegalStateException("cikis kodu $code ($snippet)"))
            }
        } catch (e: Exception) {
            Result.failure(e)
        }
    }

    private fun resolveAndroidJreAsset(): JreCandidate? {
        return try {
            val conn = (URL("https://api.github.com/repos/AngelAuraMC/angelauramc-openjdk-build/releases?per_page=10").openConnection() as HttpURLConnection).apply {
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
                    val relMeta = (rel.optString("name") + " " + rel.optString("tag_name")).lowercase()
                    if (relMeta.contains("ios") || relMeta.contains("macos") || relMeta.contains("darwin") || relMeta.contains("windows")) {
                        continue
                    }
                    val assets = rel.optJSONArray("assets") ?: continue
                    for (j in 0 until assets.length()) {
                        val asset = assets.getJSONObject(j)
                        val rawName = asset.optString("name")
                        val name = rawName.lowercase()
                        val matchesArch = name.contains("arm64") || name.contains("aarch64")
                        val isOtherPlatform = name.contains("ios") ||
                            name.contains("macos") ||
                            name.contains("darwin") ||
                            name.contains("windows") ||
                            name.contains("x86") ||
                            name.contains("arm32") ||
                            name.contains("armeabi") ||
                            (name.contains("-arm.") && !name.contains("arm64"))
                        val matchesExt = name.endsWith(".tar.xz") || name.endsWith(".tar.gz") || name.endsWith(".tgz") || name.endsWith(".zip")
                        if (name.contains(preferredTag) && matchesArch && !isOtherPlatform && matchesExt) {
                            return JreCandidate(rawName, asset.optString("browser_download_url"))
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
                            makeFileExecutable(outFile)
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

    private fun makeFileExecutable(file: File) {
        try {
            Os.chmod(file.absolutePath, 493) // 0755
        } catch (_: Exception) {
            file.setExecutable(true, false)
        }
    }

    private fun markExecutablesRecursive(dir: File) {
        if (!dir.exists()) return
        dir.listFiles()?.forEach { f ->
            if (f.isDirectory) {
                markExecutablesRecursive(f)
            } else {
                makeFileExecutable(f)
            }
        }
    }

    private fun markSharedLibsRecursive(dir: File) {
        if (!dir.exists()) return
        dir.listFiles()?.forEach { f ->
            if (f.isDirectory) {
                markSharedLibsRecursive(f)
            } else if (f.name.endsWith(".so") || f.name.contains(".so.")) {
                makeFileExecutable(f)
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
        var currentUrl = urlStr
        var redirects = 0
        while (redirects < 6) {
            val conn = (URL(currentUrl).openConnection() as HttpURLConnection).apply {
                setRequestProperty("User-Agent", "MC-SekaiLink-Android/0.1.0")
                instanceFollowRedirects = false
                connectTimeout = 15_000
                readTimeout = 120_000
            }
            val code = conn.responseCode
            if (code in 301..303 || code == 307 || code == 308) {
                val next = conn.getHeaderField("Location")
                conn.disconnect()
                if (next.isNullOrBlank()) {
                    throw IOException("Gecersiz yonlendirme HTTP $code")
                }
                currentUrl = URL(URL(currentUrl), next).toString()
                redirects++
                continue
            }
            if (code != HttpURLConnection.HTTP_OK) {
                conn.disconnect()
                throw IOException("Indirme hatasi HTTP $code")
            }
            conn.inputStream.use { input ->
                FileOutputStream(dest).use { out ->
                    input.copyTo(out)
                }
            }
            conn.disconnect()
            return
        }
        throw IOException("Cok fazla HTTP yonlendirmesi")
    }
}
