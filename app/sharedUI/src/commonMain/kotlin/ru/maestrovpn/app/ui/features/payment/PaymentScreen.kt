@file:OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)

package ru.maestrovpn.app.ui.features.payment

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material.icons.rounded.ErrorOutline
import androidx.compose.material.icons.rounded.HourglassTop
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ru.maestrovpn.app.data.payment.ConnectionConfig
import ru.maestrovpn.app.data.payment.Tariff

/**
 * PaymentScreen renders the in-app subscription purchase flow driven by
 * [PaymentViewModel]: pick a tariff, follow the phone-transfer instructions,
 * confirm payment, then wait for the operator to approve before the access
 * token is shown. The same layout works on phones and Android TV because it is
 * a single scrollable column of large, focusable controls.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PaymentScreen(
    viewModel: PaymentViewModel?,
    panelUrl: String,
    onPanelUrlChange: (String) -> Unit,
    onBack: () -> Unit,
    onActivated: (token: String, config: ConnectionConfig?) -> Unit = { _, _ -> }
) {
    if (viewModel == null) {
        PanelUrlPrompt(panelUrl = panelUrl, onPanelUrlChange = onPanelUrlChange, onBack = onBack)
        return
    }

    val state by viewModel.state.collectAsState()

    LaunchedEffect(Unit) { viewModel.loadTariffs() }
    LaunchedEffect(state.step, state.token) {
        if (state.step == PaymentStep.Active && state.token.isNotEmpty()) {
            onActivated(state.token, state.connectionConfig)
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Подписка") },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Назад")
                    }
                }
            )
        }
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .imePadding()
                .navigationBarsPadding()
                .padding(horizontal = 20.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp)
        ) {
            when (state.step) {
                PaymentStep.ChooseTariff -> ChooseTariffSection(state, viewModel)
                PaymentStep.AwaitingPayment -> AwaitingPaymentSection(state, viewModel)
                PaymentStep.PendingApproval -> StatusSection(
                    icon = Icons.Rounded.HourglassTop,
                    title = "Ожидаем подтверждения",
                    message = "Мы уведомили оператора о вашей оплате. Доступ откроется " +
                        "автоматически после подтверждения — обычно в течение нескольких минут.",
                    showSpinner = true
                )
                PaymentStep.Active -> ActiveSection(state, onBack)
                PaymentStep.Rejected -> StatusSection(
                    icon = Icons.Rounded.ErrorOutline,
                    title = "Оплата не подтверждена",
                    message = "Оператор отклонил заявку. Проверьте перевод и попробуйте снова, " +
                        "либо свяжитесь с поддержкой.",
                    action = "Начать заново" to { viewModel.reset() }
                )
            }

            state.error?.let { error ->
                Text(
                    text = error,
                    color = MaterialTheme.colorScheme.error,
                    style = MaterialTheme.typography.bodyMedium
                )
            }
        }
    }
}

/**
 * Shown when no panel URL is configured yet: lets the user enter the operator
 * panel address before the subscription flow can talk to the server.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun PanelUrlPrompt(
    panelUrl: String,
    onPanelUrlChange: (String) -> Unit,
    onBack: () -> Unit
) {
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Подписка") },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Назад")
                    }
                }
            )
        }
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .imePadding()
                .navigationBarsPadding()
                .padding(horizontal = 20.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp)
        ) {
            Text(
                text = "Адрес панели не настроен",
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.SemiBold
            )
            Text(
                text = "Укажите адрес сервера-панели, выданный продавцом, чтобы " +
                    "оформить подписку. Например: http://example.com:8090",
                style = MaterialTheme.typography.bodyMedium
            )
            OutlinedTextField(
                value = panelUrl,
                onValueChange = onPanelUrlChange,
                label = { Text("Адрес панели") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(
                    keyboardType = KeyboardType.Uri,
                    imeAction = ImeAction.Done
                ),
                modifier = Modifier.fillMaxWidth()
            )
        }
    }
}

@Composable
private fun ChooseTariffSection(state: PaymentState, viewModel: PaymentViewModel) {
    val isRenew = !state.knownLogin.isNullOrBlank()

    Text(
        text = if (isRenew) "Продление подписки" else "Выберите тариф",
        style = MaterialTheme.typography.headlineSmall,
        fontWeight = FontWeight.SemiBold
    )

    if (isRenew) {
        AccountCard(state, viewModel)
    }
    if (state.deviceLimitReached) {
        DeviceLimitBanner(state, viewModel)
    }

    if (!isRenew) {
        OutlinedTextField(
            value = state.login,
            onValueChange = viewModel::onLoginChanged,
            label = { Text("Логин (придумайте имя)") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(
                keyboardType = KeyboardType.Text,
                imeAction = ImeAction.Done
            ),
            modifier = Modifier.fillMaxWidth()
        )
    }

    if (state.loading && state.tariffs.isEmpty()) {
        CircularProgressIndicator(modifier = Modifier.padding(8.dp))
    }

    Text(
        text = if (isRenew) "Выберите срок продления:" else "Тарифы:",
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant
    )
    state.tariffs.forEach { tariff ->
        TariffCard(
            tariff = tariff,
            enabled = !state.loading,
            onClick = { viewModel.purchase(tariff) }
        )
    }

    TransferByLogin(state, viewModel)
}

@Composable
private fun AccountCard(state: PaymentState, viewModel: PaymentViewModel) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant)
    ) {
        Column(
            modifier = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp)
        ) {
            Text("Аккаунт: ${state.knownLogin}", fontWeight = FontWeight.SemiBold)
            if (state.expires.isNotBlank()) {
                Text("Действует до: ${state.expires}", style = MaterialTheme.typography.bodySmall)
            }
            Text(
                "Устройств: ${state.devices}/${state.deviceLimit}",
                style = MaterialTheme.typography.bodySmall
            )
            OutlinedButton(onClick = { viewModel.resetDevices() }, enabled = !state.loading) {
                Text("Сбросить устройства")
            }
            state.info?.let {
                Text(it, color = MaterialTheme.colorScheme.primary, style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

@Composable
private fun DeviceLimitBanner(state: PaymentState, viewModel: PaymentViewModel) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.errorContainer)
    ) {
        Column(
            modifier = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            Text(
                "Достигнут лимит устройств (${state.deviceLimit}). Сбросьте устройства, чтобы подключить это.",
                color = MaterialTheme.colorScheme.onErrorContainer,
                style = MaterialTheme.typography.bodyMedium
            )
            Button(onClick = { viewModel.resetDevices() }, enabled = !state.loading) {
                Text("Сбросить устройства")
            }
        }
    }
}

@Composable
private fun TransferByLogin(state: PaymentState, viewModel: PaymentViewModel) {
    var loginInput by remember { mutableStateOf("") }
    Spacer(modifier = Modifier.height(8.dp))
    Text(
        "Перенести подписку с другого устройства (ТВ/телефон):",
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant
    )
    OutlinedTextField(
        value = loginInput,
        onValueChange = { loginInput = it },
        label = { Text("Логин с другого устройства") },
        singleLine = true,
        keyboardOptions = KeyboardOptions(
            keyboardType = KeyboardType.Text,
            imeAction = ImeAction.Done
        ),
        modifier = Modifier.fillMaxWidth()
    )
    OutlinedButton(
        onClick = { viewModel.activateByLogin(loginInput) },
        enabled = !state.loading && loginInput.isNotBlank(),
        modifier = Modifier.fillMaxWidth()
    ) {
        Text("Подключить это устройство")
    }
}

@Composable
private fun TariffCard(tariff: Tariff, enabled: Boolean, onClick: () -> Unit) {
    Card(
        onClick = onClick,
        enabled = enabled,
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.surfaceVariant
        )
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(20.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween
        ) {
            Column {
                Text(
                    text = tariff.title.ifBlank { "${tariff.months} мес." },
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold
                )
                Text(
                    text = "${tariff.months} мес.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
            Text(
                text = "${tariff.priceRub} ₽",
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold
            )
        }
    }
}

@Composable
private fun AwaitingPaymentSection(state: PaymentState, viewModel: PaymentViewModel) {
    Text(
        text = "Оплата переводом",
        style = MaterialTheme.typography.headlineSmall,
        fontWeight = FontWeight.SemiBold
    )
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.surfaceVariant
        )
    ) {
        Column(
            modifier = Modifier.padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            if (state.payInfo.isNotBlank()) {
                Text(
                    text = state.payInfo,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold
                )
            }
            if (state.instructions.isNotBlank()) {
                Text(text = state.instructions, fontSize = 15.sp)
            }
            Text(
                text = "Ваш логин: ${state.login}",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
        }
    }
    Text(
        text = "После перевода нажмите «Я оплатил». Оператор получит уведомление и " +
            "подтвердит доступ.",
        style = MaterialTheme.typography.bodyMedium
    )
    Button(
        onClick = { viewModel.confirmPaid() },
        enabled = !state.loading,
        modifier = Modifier
            .fillMaxWidth()
            .height(56.dp)
    ) {
        if (state.loading) {
            CircularProgressIndicator(
                modifier = Modifier.height(22.dp),
                strokeWidth = 2.dp
            )
        } else {
            Text("Я оплатил", fontSize = 16.sp)
        }
    }
    OutlinedButton(
        onClick = { viewModel.reset() },
        modifier = Modifier.fillMaxWidth()
    ) {
        Text("Выбрать другой тариф")
    }
}

@Composable
private fun ActiveSection(state: PaymentState, onBack: () -> Unit) {
    StatusSection(
        icon = Icons.Rounded.CheckCircle,
        title = "Доступ активен",
        message = buildString {
            append("Подписка оформлена. Токен подставлен в настройки подключения.")
            if (state.expires.isNotBlank()) append("\nДействует до: ${state.expires}")
        },
        action = "Готово" to onBack
    )
}

@Composable
private fun StatusSection(
    icon: ImageVector,
    title: String,
    message: String,
    showSpinner: Boolean = false,
    action: Pair<String, () -> Unit>? = null
) {
    Column(
        modifier = Modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(16.dp)
    ) {
        Spacer(Modifier.height(24.dp))
        Icon(
            imageVector = icon,
            contentDescription = null,
            modifier = Modifier.height(72.dp),
            tint = MaterialTheme.colorScheme.primary
        )
        Text(
            text = title,
            style = MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.SemiBold
        )
        Text(
            text = message,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant
        )
        if (showSpinner) {
            CircularProgressIndicator()
        }
        action?.let { (label, onClick) ->
            Button(
                onClick = onClick,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(56.dp)
            ) {
                Text(label, fontSize = 16.sp)
            }
        }
    }
}
