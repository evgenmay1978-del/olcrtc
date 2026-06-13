package ru.maestrovpn.app.ui.features.payment

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import ru.maestrovpn.app.data.payment.ConnectionConfig
import ru.maestrovpn.app.data.payment.PaymentApi
import ru.maestrovpn.app.data.payment.SignupResponse
import ru.maestrovpn.app.data.payment.StatusResponse
import ru.maestrovpn.app.data.payment.Tariff
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals

private class FakePaymentApi(
    private val statusValue: StatusResponse = StatusResponse(status = StatusResponse.STATUS_ACTIVE, token = "tok"),
    private val configValue: ConnectionConfig = ConnectionConfig()
) : PaymentApi {
    var paidCalled = false
    override suspend fun tariffs(): List<Tariff> =
        listOf(Tariff("1m", 1, 400, "1 месяц"), Tariff("3m", 3, 1100, "3 месяца"))

    override suspend fun signup(login: String, tariffId: String) =
        SignupResponse(login = login, tariff = Tariff(tariffId, 3, 1100, "3 месяца"), payInfo = "pay +7", message = "transfer 1100")

    override suspend fun markPaid(login: String) { paidCalled = true }
    override suspend fun status(login: String) = statusValue
    override suspend fun config(login: String) = configValue
    var renewCalled = false
    var resetCalled = false
    override suspend fun renew(login: String, tariffId: String): SignupResponse {
        renewCalled = true
        return SignupResponse(login = login, payInfo = "pay +7", message = "renew")
    }
    override suspend fun resetDevices(login: String) { resetCalled = true }
}

@OptIn(ExperimentalCoroutinesApi::class)
class PaymentViewModelTest {
    private val testDispatcher = StandardTestDispatcher()

    @BeforeTest fun setUp() = Dispatchers.setMain(testDispatcher)
    @AfterTest fun tearDown() = Dispatchers.resetMain()

    @Test
    fun loadsTariffs() = runTest(testDispatcher) {
        val vm = PaymentViewModel(FakePaymentApi(), testDispatcher)
        vm.loadTariffs()
        advanceUntilIdle()
        assertEquals(2, vm.state.value.tariffs.size)
    }

    @Test
    fun signupRequiresLogin() = runTest(testDispatcher) {
        val vm = PaymentViewModel(FakePaymentApi(), testDispatcher)
        vm.signup(Tariff("1m", 1, 400, "1 месяц"))
        advanceUntilIdle()
        // No login -> stays on tariff selection with an error.
        assertEquals(PaymentStep.ChooseTariff, vm.state.value.step)
        assertEquals("Введите логин", vm.state.value.error)
    }

    @Test
    fun signupThenPaidThenActive() = runTest(testDispatcher) {
        val api = FakePaymentApi()
        val vm = PaymentViewModel(api, testDispatcher)
        vm.onLoginChanged("maria")

        vm.signup(Tariff("3m", 3, 1100, "3 месяца"))
        advanceUntilIdle()
        assertEquals(PaymentStep.AwaitingPayment, vm.state.value.step)
        assertEquals("pay +7", vm.state.value.payInfo)

        vm.confirmPaid()
        advanceUntilIdle()
        // markPaid was called; polling sees active -> Active with token.
        assertEquals(true, api.paidCalled)
        assertEquals(PaymentStep.Active, vm.state.value.step)
        assertEquals("tok", vm.state.value.token)
    }

    @Test
    fun rejectedStatusEndsFlow() = runTest(testDispatcher) {
        val vm = PaymentViewModel(
            FakePaymentApi(StatusResponse(status = StatusResponse.STATUS_REJECTED)),
            testDispatcher
        )
        vm.onLoginChanged("bob")
        vm.signup(Tariff("1m", 1, 400, "1 месяц"))
        advanceUntilIdle()
        vm.confirmPaid()
        advanceUntilIdle()
        assertEquals(PaymentStep.Rejected, vm.state.value.step)
    }
}
