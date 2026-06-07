package org.olcbox.app.data.payment

import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import io.ktor.http.HttpHeaders
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import kotlin.test.assertFailsWith

class PaymentApiTest {
    private fun client(handler: (path: String) -> Pair<HttpStatusCode, String>): HttpClient {
        return HttpClient(MockEngine { request ->
            val (code, body) = handler(request.url.encodedPath + (request.url.encodedQuery.let { if (it.isEmpty()) "" else "?$it" }))
            respond(
                content = body,
                status = code,
                headers = headersOf(HttpHeaders.ContentType, "application/json")
            )
        })
    }

    @Test
    fun parsesTariffs() = runTest {
        val api = HttpPaymentApi(
            "http://panel",
            client { _ ->
                HttpStatusCode.OK to """{"tariffs":[
                    {"id":"1m","months":1,"priceRub":400,"title":"1 месяц"},
                    {"id":"6m","months":6,"priceRub":2200,"title":"6 месяцев"}
                ]}"""
            }
        )
        val tariffs = api.tariffs()
        assertEquals(2, tariffs.size)
        assertEquals(400, tariffs[0].priceRub)
        assertEquals("6m", tariffs[1].id)
    }

    @Test
    fun signupReturnsPayInfo() = runTest {
        val api = HttpPaymentApi(
            "http://panel",
            client { _ ->
                HttpStatusCode.OK to """{"login":"maria","tariff":{"id":"3m","months":3,"priceRub":1100,"title":"3 месяца"},"payInfo":"pay to +7...","message":"transfer 1100"}"""
            }
        )
        val resp = api.signup("maria", "3m")
        assertEquals("maria", resp.login)
        assertEquals(1100, resp.tariff.priceRub)
        assertTrue(resp.payInfo.contains("+7"))
    }

    @Test
    fun statusReportsPendingThenActive() = runTest {
        val pending = HttpPaymentApi("http://panel", client { _ ->
            HttpStatusCode.OK to """{"status":"pending","expires":"2026-09-02"}"""
        })
        assertEquals(StatusResponse.STATUS_PENDING, pending.status("maria").status)

        val active = HttpPaymentApi("http://panel", client { _ ->
            HttpStatusCode.OK to """{"status":"active","expires":"2026-09-02","token":"abc123"}"""
        })
        val s = active.status("maria")
        assertEquals(StatusResponse.STATUS_ACTIVE, s.status)
        assertEquals("abc123", s.token)
    }

    @Test
    fun nonSuccessStatusThrows() = runTest {
        val api = HttpPaymentApi("http://panel", client { _ ->
            HttpStatusCode.Conflict to """{"error":"login already exists"}"""
        })
        assertFailsWith<PaymentException> { api.signup("taken", "1m") }
    }
}
