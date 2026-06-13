package ru.maestrovpn.app.data.payment

import kotlinx.serialization.SerialName
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
    val token: String = "",
    val devices: Int = 0,
    @SerialName("device_limit")
    val deviceLimit: Int = 3
) {
    companion object {
        const val STATUS_PENDING = "pending"
        const val STATUS_ACTIVE = "active"
        const val STATUS_REJECTED = "rejected"
        const val STATUS_DISABLED = "disabled"
        const val STATUS_NOT_FOUND = "not_found"
    }
}

/**
 * Response of GET /api/config: the ready-to-connect parameters the app needs to
 * seed a working location for an active client. Room/key/provider/transport are
 * server-wide; the token is this client's own access token.
 */
@Serializable
data class ConnectionConfig(
    val name: String = "",
    val provider: String = "",
    @SerialName("room_id")
    val roomId: String = "",
    val channel: String = "",
    val key: String = "",
    val transport: String = "",
    @SerialName("engine_name")
    val engineName: String = "",
    @SerialName("engine_url")
    val engineUrl: String = "",
    val token: String = "",
    val expires: String = ""
) {
    /** True when the bundle carries enough to actually connect. */
    fun isConnectable(): Boolean = roomId.isNotBlank() && key.isNotBlank()
}
