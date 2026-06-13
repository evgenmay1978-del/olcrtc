package ru.maestrovpn.app.data.payment

import io.ktor.client.HttpClient
import io.ktor.client.request.get
import io.ktor.client.request.headers
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.client.statement.HttpResponse
import io.ktor.client.statement.bodyAsText
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.http.encodeURLParameter
import io.ktor.http.isSuccess
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/** Request body for POST /api/signup. */
@Serializable
private data class SignupRequest(val login: String, val tariff: String)

/** Request body for POST /api/paid. */
@Serializable
private data class PaidRequest(val login: String)

/** Request body for POST /api/renew. */
@Serializable
private data class RenewRequest(val login: String, val tariff: String)

/** Request body for POST /api/reset-devices. */
@Serializable
private data class ResetRequest(val login: String)

/** Shape of a device_limit error body. */
@Serializable
private data class DeviceLimitError(
    val error: String = "",
    @SerialName("device_limit") val deviceLimit: Int = 3
)

/** Thrown when a payment API call fails. */
open class PaymentException(message: String) : Exception(message)

/** Thrown when binding a device would exceed the account's device limit. */
class DeviceLimitException(val limit: Int) : PaymentException("device_limit")

/** Client-facing payment operations, abstracted for testability. */
interface PaymentApi {
    suspend fun tariffs(): List<Tariff>
    suspend fun signup(login: String, tariffId: String): SignupResponse
    suspend fun markPaid(login: String)
    suspend fun status(login: String): StatusResponse
    suspend fun config(login: String): ConnectionConfig
    suspend fun renew(login: String, tariffId: String): SignupResponse
    suspend fun resetDevices(login: String)
}

/**
 * HttpPaymentApi talks to the olcRTC server's client-facing payment endpoints
 * (/api/tariffs, /api/signup, /api/paid, /api/status, /api/config, /api/renew,
 * /api/reset-devices). baseUrl is the panel's address. hwidProvider yields this
 * device's stable id, sent as x-hwid on /api/config so the panel can bind the
 * device under the strict per-account device cap.
 */
class HttpPaymentApi(
    private val baseUrl: String,
    private val httpClient: HttpClient,
    private val hwidProvider: suspend () -> String = { "" },
    private val json: Json = defaultJson
) : PaymentApi {
    override suspend fun tariffs(): List<Tariff> {
        val text = getText("/api/tariffs")
        return json.decodeFromString(TariffsResponse.serializer(), text).tariffs
    }

    override suspend fun signup(login: String, tariffId: String): SignupResponse {
        val body = json.encodeToString(
            SignupRequest.serializer(),
            SignupRequest(login = login, tariff = tariffId)
        )
        val text = postJson("/api/signup", body)
        return json.decodeFromString(SignupResponse.serializer(), text)
    }

    /** Reports that the client paid; the operator is notified to approve. */
    override suspend fun markPaid(login: String) {
        val body = json.encodeToString(PaidRequest.serializer(), PaidRequest(login = login))
        postJson("/api/paid", body)
    }

    override suspend fun status(login: String): StatusResponse {
        val text = getText("/api/status?login=" + login.encodeURLParameter())
        return json.decodeFromString(StatusResponse.serializer(), text)
    }

    /**
     * Fetches ready-to-connect parameters for an active client and binds this
     * device (via x-hwid) under the account's device cap. Throws
     * [DeviceLimitException] when the cap is reached.
     */
    override suspend fun config(login: String): ConnectionConfig {
        val hwid = runCatching { hwidProvider() }.getOrDefault("")
        val response = httpClient.get(
            baseUrl.trimEnd('/') + "/api/config?login=" + login.encodeURLParameter()
        ) {
            if (hwid.isNotBlank()) {
                headers { append("x-hwid", hwid) }
            }
        }
        if (!response.status.isSuccess()) {
            throwForResponse(response)
        }
        return json.decodeFromString(ConnectionConfig.serializer(), response.bodyAsText())
    }

    /** Re-purchases for an existing login (the same account is extended). */
    override suspend fun renew(login: String, tariffId: String): SignupResponse {
        val body = json.encodeToString(
            RenewRequest.serializer(),
            RenewRequest(login = login, tariff = tariffId)
        )
        val text = postJson("/api/renew", body)
        return json.decodeFromString(SignupResponse.serializer(), text)
    }

    /** Clears the account's bound devices so new ones can be added. */
    override suspend fun resetDevices(login: String) {
        val body = json.encodeToString(ResetRequest.serializer(), ResetRequest(login = login))
        postJson("/api/reset-devices", body)
    }

    private suspend fun getText(path: String): String {
        val response = httpClient.get(baseUrl.trimEnd('/') + path)
        if (!response.status.isSuccess()) {
            throwForResponse(response)
        }
        return response.bodyAsText()
    }

    private suspend fun postJson(path: String, body: String): String {
        val response = httpClient.post(baseUrl.trimEnd('/') + path) {
            contentType(ContentType.Application.Json)
            setBody(body)
        }
        if (!response.status.isSuccess()) {
            throwForResponse(response)
        }
        return response.bodyAsText()
    }

    /** Maps a non-2xx response to a typed exception, surfacing device_limit. */
    private suspend fun throwForResponse(response: HttpResponse): Nothing {
        val body = runCatching { response.bodyAsText() }.getOrDefault("")
        if (body.contains("device_limit")) {
            val limit = runCatching {
                json.decodeFromString(DeviceLimitError.serializer(), body).deviceLimit
            }.getOrDefault(3)
            throw DeviceLimitException(limit)
        }
        throw PaymentException("server returned ${response.status.value}")
    }

    private companion object {
        val defaultJson = Json { ignoreUnknownKeys = true }
    }
}
