package ru.maestrovpn.app.data.payment

import ru.maestrovpn.app.data.datasource.createProxyHttpClient

/**
 * Builds an [HttpPaymentApi] pointed at the operator panel. [baseUrl] is the
 * panel's address, e.g. "http://194.48.141.106:8090". Returns null when no
 * panel URL is configured so callers can prompt the user instead of failing.
 */
fun createPaymentApi(
    baseUrl: String,
    hwidProvider: suspend () -> String = { "" }
): PaymentApi? {
    val trimmed = baseUrl.trim()
    if (trimmed.isEmpty()) return null
    return HttpPaymentApi(trimmed, createProxyHttpClient(), hwidProvider)
}
