package ru.maestrovpn.app.ui.features.payment

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.IO
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import ru.maestrovpn.app.data.payment.ConnectionConfig
import ru.maestrovpn.app.data.payment.PaymentApi
import ru.maestrovpn.app.data.payment.SignupResponse
import ru.maestrovpn.app.data.payment.StatusResponse
import ru.maestrovpn.app.data.payment.Tariff

/** Steps of the in-app purchase flow. */
enum class PaymentStep {
    /** Choosing a tariff. */
    ChooseTariff,

    /** Showing payment instructions; waiting for the user to pay and confirm. */
    AwaitingPayment,

    /** Payment claimed; waiting for the operator to approve. */
    PendingApproval,

    /** Approved: access is active and the token is available. */
    Active,

    /** Operator rejected the payment. */
    Rejected
}

/** Immutable UI state for the payment screen. */
data class PaymentState(
    val step: PaymentStep = PaymentStep.ChooseTariff,
    val tariffs: List<Tariff> = emptyList(),
    val login: String = "",
    val selectedTariff: Tariff? = null,
    val payInfo: String = "",
    val instructions: String = "",
    val token: String = "",
    val expires: String = "",
    val connectionConfig: ConnectionConfig? = null,
    val loading: Boolean = false,
    val error: String? = null
)

/**
 * PaymentViewModel drives the purchase flow against the server's payment API:
 * load tariffs -> sign up (creates a pending client, returns pay info) ->
 * "I paid" (notifies the operator) -> poll status until active or rejected.
 */
class PaymentViewModel(
    private val api: PaymentApi,
    private val ioDispatcher: CoroutineDispatcher = Dispatchers.IO
) : ViewModel() {

    private val _state = MutableStateFlow(PaymentState())
    val state get() = _state.asStateFlow()

    fun onLoginChanged(value: String) {
        _state.update { it.copy(login = value, error = null) }
    }

    /** Returns to tariff selection, keeping the entered login. */
    fun reset() {
        _state.update {
            PaymentState(login = it.login, tariffs = it.tariffs)
        }
    }

    /** Loads the tariff catalog for the picker. */
    fun loadTariffs() {
        _state.update { it.copy(loading = true, error = null) }
        viewModelScope.launch {
            runCatching { withContext(ioDispatcher) { api.tariffs() } }
                .onSuccess { tariffs -> _state.update { it.copy(loading = false, tariffs = tariffs) } }
                .onFailure { e -> _state.update { it.copy(loading = false, error = e.message ?: "load failed") } }
        }
    }

    /** Signs the user up for the chosen tariff and shows payment instructions. */
    fun signup(tariff: Tariff) {
        val login = _state.value.login.trim()
        if (login.isEmpty()) {
            _state.update { it.copy(error = "Введите логин") }
            return
        }
        _state.update { it.copy(loading = true, error = null, selectedTariff = tariff) }
        viewModelScope.launch {
            runCatching { withContext(ioDispatcher) { api.signup(login, tariff.id) } }
                .onSuccess { resp -> applySignup(resp) }
                .onFailure { e -> _state.update { it.copy(loading = false, error = e.message ?: "signup failed") } }
        }
    }

    private fun applySignup(resp: SignupResponse) {
        _state.update {
            it.copy(
                loading = false,
                step = PaymentStep.AwaitingPayment,
                payInfo = resp.payInfo,
                instructions = resp.message
            )
        }
    }

    /** Reports payment and moves to waiting-for-approval, then polls status. */
    fun confirmPaid() {
        val login = _state.value.login.trim()
        _state.update { it.copy(loading = true, error = null) }
        viewModelScope.launch {
            runCatching { withContext(ioDispatcher) { api.markPaid(login) } }
                .onSuccess {
                    _state.update { it.copy(loading = false, step = PaymentStep.PendingApproval) }
                    pollStatus()
                }
                .onFailure { e -> _state.update { it.copy(loading = false, error = e.message ?: "request failed") } }
        }
    }

    /** Polls the server until the client becomes active or rejected. */
    fun pollStatus() {
        val login = _state.value.login.trim()
        if (login.isEmpty()) return
        viewModelScope.launch {
            repeat(POLL_ATTEMPTS) {
                val resp = runCatching { withContext(ioDispatcher) { api.status(login) } }.getOrNull()
                if (resp != null) {
                    when (resp.status) {
                        StatusResponse.STATUS_ACTIVE -> {
                            activate(login, resp)
                            return@launch
                        }
                        StatusResponse.STATUS_REJECTED -> {
                            _state.update { it.copy(step = PaymentStep.Rejected) }
                            return@launch
                        }
                    }
                }
                delay(POLL_INTERVAL_MS)
            }
        }
    }

    /**
     * Marks access active and fetches the ready-to-connect parameters so the app
     * can seed a working location. The config fetch is best-effort: if it fails
     * (older panel without /api/config), the flow still completes with the token
     * and the app falls back to attaching the token to existing locations.
     */
    private suspend fun activate(login: String, resp: StatusResponse) {
        val config = runCatching { withContext(ioDispatcher) { api.config(login) } }
            .getOrNull()
            ?.takeIf { it.isConnectable() }
        _state.update {
            it.copy(
                step = PaymentStep.Active,
                token = resp.token,
                expires = resp.expires,
                connectionConfig = config
            )
        }
    }

    private companion object {
        const val POLL_ATTEMPTS = 120
        const val POLL_INTERVAL_MS = 5_000L
    }
}
