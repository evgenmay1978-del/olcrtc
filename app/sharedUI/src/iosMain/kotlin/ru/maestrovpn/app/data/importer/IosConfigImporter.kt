package ru.maestrovpn.app.data.importer

import ru.maestrovpn.app.ios.IosPlatformBridge

class IosConfigImporter(
    private val platformBridge: IosPlatformBridge
) : ConfigImporter {
    override fun getFromClipboard(): String? {
        return platformBridge.readClipboard()?.takeIf { it.isNotBlank() }
    }

    override fun copyToClipboard(text: String) {
        platformBridge.writeClipboard(text)
        platformBridge.showMessage("Config copied")
    }

    override suspend fun readTextFromSource(source: Any): String? {
        return source as? String
    }
}
