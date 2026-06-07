package org.olcbox.app.ui.features.payment

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.olcbox.app.data.payment.PaymentApi
import org.olcbox.app.data.payment.SignupResponse
import org.olcbox.app.data.payment.StatusResponse
import org.olcbox.app.data.payment.Tariff
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals

private class FakePaymentApi(
    private val statusValue: StatusResponse = StatusResponse(status = StatusResponse.STATUS_ACTIVE, token = "tok")
) : PaymentApi {
    var paidCalled = false
    override suspend fun tariffs(): List<Tariff> =
        listOf(Tariff("1m", 1, 400, "1 месяц"), Tariff("3m", 3, 1100, "3 месяца"))

    override suspend fun signup(login: String, tariffId: String) =
        SignupResponse(login = login, tariff = Tariff(tariffId, 3, 1100, "3 месяца"), payInfo = "pay +7", message = "transfer 1100")

    override suspend fun markPaid(login: String) { paidCalled = true }
    override suspend fun status(login: String) = statusValue
}

class PaymentViewModelTest {
    @BeforeTest fun setUp() = Dispatchers.setMain(StandardTestDispatcher())
    @AfterTest fun tearDown() = Dispatchers.resetMain()

    @Test
    fun loadsTariffs() = runTest {
        val vm = PaymentViewModel(FakePaymentApi())
        vm.loadTariffs()
        advanceUntilIdle()
        assertEquals(2, vm.state.value.tariffs.size)
    }

    @Test
    fun signupRequiresLogin() = runTest {
        val vm = PaymentViewModel(FakePaymentApi())
        vm.signup(Tariff("1m", 1, 400, "1 месяц"))
        advanceUntilIdle()
        // No login -> stays on tariff selection with an error.
        assertEquals(PaymentStep.ChooseTariff, vm.state.value.step)
        assertEquals("Введите логин", vm.state.value.error)
    }

    @Test
    fun signupThenPaidThenActive() = runTest {
        val api = FakePaymentApi()
        val vm = PaymentViewModel(api)
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
    fun rejectedStatusEndsFlow() = runTest {
        val vm = PaymentViewModel(
            FakePaymentApi(StatusResponse(status = StatusResponse.STATUS_REJECTED))
        )
        vm.onLoginChanged("bob")
        vm.signup(Tariff("1m", 1, 400, "1 месяц"))
        advanceUntilIdle()
        vm.confirmPaid()
        advanceUntilIdle()
        assertEquals(PaymentStep.Rejected, vm.state.value.step)
    }
}
