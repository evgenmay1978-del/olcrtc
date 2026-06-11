package org.olcbox.app.vpn

import android.content.Context
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.atomic.AtomicReference

/**
 * Source of truth for the "bypass VPN for Russian apps" preset.
 *
 * A small built-in list ships with the app as a safety net; the live list is
 * fetched from the project repository (app/russian-apps.txt) and cached on disk,
 * so the operator can grow it by editing one file in the repo — no new app build
 * required. Format per line: "*prefix" matches by package prefix, "#" is a
 * comment, anything else is an exact package name.
 */
object RussianAppList {
    private const val REMOTE_URL =
        "https://raw.githubusercontent.com/evgenmay1978-del/olcrtc/main/app/russian-apps.txt"
    private const val CACHE_FILE = "russian-apps.txt"

    private data class Rules(val names: Set<String>, val prefixes: List<String>)

    private val defaults = Rules(
        names = setOf(
            "ru.sberbankmobile",
            "ru.ozon.app.android",
            "ru.avito",
            "ru.vtb24.mobilebanking.android",
            "ru.tinkoff.mb",
        ),
        prefixes = listOf("ru.", "com.yandex."),
    )

    private val current = AtomicReference(defaults)

    /** True if [packageName] belongs to a Russian app that should bypass the VPN. */
    fun matches(packageName: String): Boolean {
        val pkg = packageName.lowercase()
        val rules = current.get()
        return pkg in rules.names || rules.prefixes.any { pkg.startsWith(it) }
    }

    /** Load a previously cached list from disk. Safe to call on the main thread. */
    fun loadCached(context: Context) {
        runCatching {
            val file = File(context.filesDir, CACHE_FILE)
            if (file.exists()) parse(file.readText())?.let { current.set(it) }
        }
    }

    /** Fetch the latest list from the repo and cache it. MUST run off the main thread. */
    fun refresh(context: Context): Boolean = runCatching {
        val text = download() ?: return false
        val rules = parse(text) ?: return false
        File(context.filesDir, CACHE_FILE).writeText(text)
        current.set(rules)
        true
    }.getOrDefault(false)

    private fun download(): String? {
        val conn = (URL(REMOTE_URL).openConnection() as HttpURLConnection).apply {
            connectTimeout = 10_000
            readTimeout = 10_000
            requestMethod = "GET"
        }
        return try {
            if (conn.responseCode != HttpURLConnection.HTTP_OK) {
                null
            } else {
                conn.inputStream.bufferedReader().use { it.readText() }
            }
        } finally {
            conn.disconnect()
        }
    }

    private fun parse(text: String): Rules? {
        val names = mutableSetOf<String>()
        val prefixes = mutableListOf<String>()
        text.lineSequence().forEach { raw ->
            val line = raw.trim()
            if (line.isEmpty() || line.startsWith("#")) return@forEach
            if (line.startsWith("*")) {
                line.removePrefix("*").trim().lowercase()
                    .takeIf { it.isNotEmpty() }
                    ?.let { prefixes.add(it) }
            } else {
                names.add(line.lowercase())
            }
        }
        if (names.isEmpty() && prefixes.isEmpty()) return null
        // Always keep the built-in prefixes as a safety net.
        defaults.prefixes.forEach { if (it !in prefixes) prefixes.add(it) }
        return Rules(names, prefixes)
    }
}
