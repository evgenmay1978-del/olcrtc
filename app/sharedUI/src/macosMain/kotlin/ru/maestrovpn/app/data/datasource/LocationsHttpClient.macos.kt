package ru.maestrovpn.app.data.datasource

import io.ktor.client.HttpClient
import io.ktor.client.plugins.HttpTimeout
import ru.maestrovpn.app.data.repository.SubscriptionFetchProxy

internal actual fun createProxyHttpClient(
    subscriptionProxy: SubscriptionFetchProxy?,
    connectTimeoutMs: Long,
    requestTimeoutMs: Long,
    socketTimeoutMs: Long
): HttpClient {
    return HttpClient {
        expectSuccess = false

        install(HttpTimeout) {
            connectTimeoutMillis = connectTimeoutMs
            requestTimeoutMillis = requestTimeoutMs
            socketTimeoutMillis = socketTimeoutMs
        }
    }
}

internal actual suspend fun <T> withProxyAuthentication(
    subscriptionProxy: SubscriptionFetchProxy?,
    block: suspend () -> T
): T = block()
