package ru.maestrovpn.app.data.payment

import kotlinx.serialization.Serializable

/** A purchasable subscription tariff, as returned by the server's /api/tariffs. */
@Serializable
data class Tariff(
    val id: String = "",
    val months: Int = 0,
    val priceRub: Int = 0,
    val title: String = ""
)

/** Response of GET /api/tariffs. */
@Serializable
data class TariffsResponse(
    val tariffs: List<Tariff> = emptyList()
)

/** Response of POST /api/signup: payment instructions for the new client. */
@Serializable
data class SignupResponse(
    val login: String = "",
    val tariff: Tariff = Tariff(),
    val payInfo: String = "",
    val message: String = ""
)

/** Response of GET /api/status: the client's current lifecycle state. */
@Serializable
data class StatusResponse(
    val status: String = "",
    val expires: String = "",
    val token: String = ""
) {
    companion object {
        const val STATUS_PENDING = "pending"
        const val STATUS_ACTIVE = "active"
        const val STATUS_REJECTED = "rejected"
        const val STATUS_DISABLED = "disabled"
        const val STATUS_NOT_FOUND = "not_found"
    }
}
