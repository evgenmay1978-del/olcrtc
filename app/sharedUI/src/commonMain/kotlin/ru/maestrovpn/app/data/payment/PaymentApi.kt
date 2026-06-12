package ru.maestrovpn.app.data.payment

import io.ktor.client.HttpClient
import io.ktor.client.request.get
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.client.statement.bodyAsText
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.http.encodeURLParameter
import io.ktor.http.isSuccess
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/** Request body for POST /api/signup. */
@Serializable
private data class SignupRequest(val login: String, val tariff: String)

/** Request body for POST /api/paid. */
@Serializable
private data class PaidRequest(val login: String)

/** Thrown when a payment API call fails. */
class PaymentException(message: String) : Exception(message)

/** Client-facing payment operations, abstracted for testability. */
interface PaymentApi {
    suspend fun tariffs(): List<Tariff>
    suspend fun signup(login: String, tariffId: String): SignupResponse
    suspend fun markPaid(login: String)
    suspend fun status(login: String): StatusResponse
}

/**
 * HttpPaymentApi talks to the olcRTC server's client-facing payment endpoints
 * (/api/tariffs, /api/signup, /api/paid, /api/status). baseUrl is the panel's
 * address, e.g. "https://panel.example.com" or "http://10.0.2.2:8090".
 *
 * The API is intentionally small and stateless; callers poll status to sync.
 */
class HttpPaymentApi(
    private val baseUrl: String,
    private val httpClient: HttpClient,
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

    private suspend fun getText(path: String): String {
        val response = httpClient.get(baseUrl.trimEnd('/') + path)
        if (!response.status.isSuccess()) {
            throw PaymentException("server returned ${response.status.value}")
        }
        return response.bodyAsText()
    }

    private suspend fun postJson(path: String, body: String): String {
        val response = httpClient.post(baseUrl.trimEnd('/') + path) {
            contentType(ContentType.Application.Json)
            setBody(body)
        }
        if (!response.status.isSuccess()) {
            throw PaymentException("server returned ${response.status.value}")
        }
        return response.bodyAsText()
    }

    private companion object {
        val defaultJson = Json { ignoreUnknownKeys = true }
    }
}
