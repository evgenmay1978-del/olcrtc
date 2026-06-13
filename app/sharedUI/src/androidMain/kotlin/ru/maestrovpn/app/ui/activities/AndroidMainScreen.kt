package ru.maestrovpn.app.ui.activities

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.net.VpnService
import android.widget.Toast
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.ActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.launch
import ru.maestrovpn.app.data.share.ConfigShareService
import ru.maestrovpn.app.update.AndroidUpdateSettingsStore
import ru.maestrovpn.app.update.AppUpdateInfo
import ru.maestrovpn.app.update.AppUpdateSettings
import ru.maestrovpn.app.update.AppUpdateService
import ru.maestrovpn.app.update.AndroidUpdateInstaller
import ru.maestrovpn.app.update.identity
import ru.maestrovpn.app.update.isDownloaded
import ru.maestrovpn.app.update.isUpdateCheckDue
import ru.maestrovpn.app.update.shouldShowOffer
import ru.maestrovpn.app.ui.MaestroVpnAppContent
import ru.maestrovpn.app.ui.components.ApplicationUpdateOfferSheet
import ru.maestrovpn.app.data.payment.createPaymentApi
import ru.maestrovpn.app.ui.features.home.HomeScreenViewModel
import ru.maestrovpn.app.ui.features.locations.LocationViewModel
import ru.maestrovpn.app.ui.features.payment.PaymentViewModel
import ru.maestrovpn.app.ui.navigation.AppScreen
import ru.maestrovpn.app.vpn.AndroidConnectionMode
import ru.maestrovpn.app.vpn.AndroidSplitTunnelList
import ru.maestrovpn.app.vpn.AndroidSplitTunnelMode
import ru.maestrovpn.app.vpn.AndroidVpnManager

@Composable
fun AndroidMainScreen(
    viewModel: HomeScreenViewModel,
    locationViewModel: LocationViewModel,
    vpnManager: AndroidVpnManager,
    appUpdateService: AppUpdateService? = null
) {

    var currentScreenRoute by rememberSaveable { mutableStateOf("home") }
    var currentLocationId by rememberSaveable { mutableStateOf<String?>(null) }

    val currentScreen: AppScreen =
        when (currentScreenRoute) {
            "location_settings" -> AppScreen.LocationSettings(currentLocationId)
            "subscription" -> AppScreen.Subscription
            else -> AppScreen.Home
        }

    val navigate: (AppScreen) -> Unit = { screen ->
        when (screen) {
            AppScreen.Home -> {
                currentScreenRoute = "home"
                currentLocationId = null
            }
            is AppScreen.LocationSettings -> {
                currentScreenRoute = "location_settings"
                currentLocationId = screen.locationId
            }
            AppScreen.Subscription -> {
                currentScreenRoute = "subscription"
            }
        }
    }

    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val paymentPrefs = remember(context) {
        context.getSharedPreferences("maestrovpn_payment", Context.MODE_PRIVATE)
    }
    var panelBaseUrl by rememberSaveable {
        // Default to the operator's payment panel so a freshly installed app
        // shows tariffs immediately; the user can still override it.
        mutableStateOf(paymentPrefs.getString("panel_url", "https://wapmixx.ru:9443").orEmpty())
    }
    val paymentViewModel = remember(panelBaseUrl) {
        createPaymentApi(panelBaseUrl)?.let { PaymentViewModel(it) }
    }
    // Load the cached Russian-app list immediately and refresh it from the repo
    // in the background, so the "bypass Russian apps" preset stays up to date.
    LaunchedEffect(Unit) {
        kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
            ru.maestrovpn.app.vpn.RussianAppList.loadCached(context)
            ru.maestrovpn.app.vpn.RussianAppList.refresh(context)
        }
    }
    val connectionMode by vpnManager.connectionMode.collectAsState()
    val proxySettings by vpnManager.proxySettings.collectAsState()
    val splitTunnelSettings by vpnManager.splitTunnelSettings.collectAsState()
    val dynamicThemeEnabled by vpnManager.dynamicThemeEnabled.collectAsState()
    val installedApps by vpnManager.installedApps.collectAsState()
    val homeState by viewModel.state.collectAsState()
    val logs by viewModel.logs.collectAsState()
    val pendingLogSaveCallbacks = remember {
        mutableStateOf<Pair<(String) -> Unit, (String) -> Unit>?>(null)
    }
    val pendingVpnAction = remember {
        mutableStateOf<PendingVpnPermissionAction?>(null)
    }
    var isAppSettingsOpen by remember { mutableStateOf(false) }
    var appSettingsInitialRoute by remember { mutableStateOf(AppSettingsInitialRoute.Hub) }
    var shareSheetPayload by remember { mutableStateOf<Pair<String, String>?>(null) }
    var splitTunnelRestartPending by remember { mutableStateOf(false) }
    val updateSettingsStore = remember(context) {
        AndroidUpdateSettingsStore(context)
    }
    val updateInstaller = remember(context, vpnManager) {
        AndroidUpdateInstaller(context) {
            vpnManager.subscriptionFetchProxy()
        }
    }
    var updateSettings by remember { mutableStateOf(AppUpdateSettings()) }
    var updateStatusText by remember { mutableStateOf<String?>(null) }
    var updateDownloadProgress by remember { mutableStateOf<Float?>(null) }
    var updateOffer by remember { mutableStateOf<AppUpdateInfo?>(null) }
    var relaunchAfterInstall by remember { mutableStateOf(false) }
    val subscriptionShareItems = locationViewModel.locations.toList()
        .mapNotNull { item ->
            val url = item.subscriptionUrl
                ?.trim()
                ?.takeIf { it.startsWith("https://") || it.startsWith("http://") }
                ?: return@mapNotNull null
            url to item
        }
        .groupBy({ it.first }, { it.second })
        .entries
        .sortedBy { it.key }
        .map { (url, items) ->
            val metadata = items.firstNotNullOfOrNull { it.metadata?.subscription }
            ru.maestrovpn.app.data.share.SubscriptionShareItem(
                url = url,
                name = metadata?.name?.takeIf { it.isNotBlank() }
                    ?: items.first().fullName,
                updateIntervalHours = metadata?.updateIntervalHours,
                lastRefreshAtEpochMs = metadata?.lastRefreshAtEpochMs,
                locationCount = items.size
            )
        }

    val updateInstallLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result: ActivityResult ->
        if (relaunchAfterInstall && result.resultCode == Activity.RESULT_OK) {
            relaunchAfterInstall = false
            updateInstaller.relaunchIntent()?.let { intent ->
                runCatching { context.startActivity(intent) }
            }
        } else {
            relaunchAfterInstall = false
        }
    }

    fun markSplitTunnelChanged() {
        if (homeState.isVpnConnected && connectionMode == AndroidConnectionMode.Tun) {
            splitTunnelRestartPending = true
        }
    }

    fun applyPendingSplitTunnelRestart() {
        if (splitTunnelRestartPending && homeState.isVpnConnected && connectionMode == AndroidConnectionMode.Tun) {
            viewModel.restartVpnIfRunning()
        }
        splitTunnelRestartPending = false
    }

    suspend fun saveUpdateSettings(settings: AppUpdateSettings) {
        val normalized = settings.normalized()
        updateSettings = normalized
        updateSettingsStore.save(normalized)
    }

    fun showUpdateResult(info: AppUpdateInfo) {
        if (info.isDownloaded(updateSettings)) {
            updateOffer = null
            updateStatusText = "Последняя версия (${info.channel.name.lowercase()}) уже загружена"
        } else if (info.isUpdateAvailable) {
            updateOffer = info
            updateStatusText = "Доступно обновление ${info.channel.name}: ${info.version}"
        } else {
            updateOffer = null
            updateStatusText = "Установлена последняя версия MaestroVPN"
        }
    }

    fun checkUpdate(manual: Boolean) {
        val service = appUpdateService
        if (service == null) {
            updateStatusText = "Служба обновлений недоступна"
            return
        }
        scope.launch {
            val previousSettings = updateSettings
            val checkStartedAt = kotlin.time.Clock.System.now().toEpochMilliseconds()
            if (!manual && !previousSettings.isUpdateCheckDue(checkStartedAt)) return@launch

            updateStatusText = "Проверка (${previousSettings.channel.name.lowercase()})..."
            val result = service.check(
                previousSettings.channel,
                vpnManager.subscriptionFetchProxy()
            )
            val checkedAt = kotlin.time.Clock.System.now().toEpochMilliseconds()
            val checkedSettings = previousSettings.copy(lastCheckAtEpochMs = checkedAt).normalized()
            saveUpdateSettings(checkedSettings)
            result.fold(
                onSuccess = { info ->
                    if (manual || info.shouldShowOffer(previousSettings, checkedAt)) {
                        showUpdateResult(info)
                    } else {
                        updateOffer = null
                        updateStatusText = null
                    }
                },
                onFailure = { error ->
                    updateStatusText = error.message ?: "Не удалось проверить обновления"
                }
            )
        }
    }

    fun downloadUpdate(info: AppUpdateInfo) {
        scope.launch {
            if (!updateInstaller.canRequestPackageInstalls()) {
                updateInstaller.openUnknownSourcesSettings()
                updateStatusText = "Разрешите MaestroVPN устанавливать обновления и снова нажмите Скачать"
                Toast.makeText(context, updateStatusText, Toast.LENGTH_LONG).show()
                return@launch
            }

            updateDownloadProgress = 0f
            updateStatusText = "Загрузка ${info.asset.name}..."
            val result = updateInstaller.download(info.asset) { progress ->
                updateDownloadProgress = progress
            }
            val file = result.getOrElse { error ->
                updateStatusText = "Ошибка загрузки: ${error.message ?: "неизвестная ошибка"}"
                updateDownloadProgress = null
                Toast.makeText(context, updateStatusText, Toast.LENGTH_LONG).show()
                return@launch
            }
            updateStatusText = "Установка ${info.asset.name}"
            saveUpdateSettings(
                updateSettings.copy(
                    lastSeenUpdateVersion = info.identity(),
                    lastDownloadedUpdateVersion = info.identity()
                )
            )
            updateOffer = null
            updateDownloadProgress = null
            relaunchAfterInstall = true
            updateInstallLauncher.launch(updateInstaller.installIntent(file))
        }
    }

    fun postponeUpdate(info: AppUpdateInfo) {
        scope.launch {
            saveUpdateSettings(updateSettings.copy(lastSeenUpdateVersion = info.identity()))
            updateOffer = null
        }
    }

    LaunchedEffect(appUpdateService) {
        updateSettings = updateSettingsStore.load()
        // Updates now come from our own repo with matching version numbers, so a
        // launch check only surfaces a genuinely newer build. It is gated by the
        // configured interval and deduped by last-seen version, so it no longer
        // nags on every launch the way the old upstream-pointed check did.
        checkUpdate(manual = false)
    }

    fun reloadLocationsAfterImport(onComplete: () -> Unit = {}) {
        locationViewModel.loadLocations {
            viewModel.loadCurrentConfig(onComplete)
        }
    }

    val vpnRequestLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result: ActivityResult ->
        if (result.resultCode == Activity.RESULT_OK) {
            when (val action = pendingVpnAction.value) {
                PendingVpnPermissionAction.Toggle -> viewModel.ToggleVpn()
                is PendingVpnPermissionAction.RestartWithMode -> {
                    vpnManager.selectConnectionMode(action.mode)
                    viewModel.restartVpnIfRunning()
                }
                null -> Unit
            }
        }
        pendingVpnAction.value = null
    }

    val filePickerLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.GetContent()
    ) { uri: Uri? ->
        uri?.let {
            viewModel.onFileSelected(it) {
                reloadLocationsAfterImport()
            }
        }
    }

    val qrScannerLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result: ActivityResult ->
        if (result.resultCode != Activity.RESULT_OK) return@rememberLauncherForActivityResult

        val rawText = result.data?.getStringExtra(QrScannerActivity.EXTRA_QR_TEXT)
            ?.trim()
            .orEmpty()

        if (rawText.isBlank()) return@rememberLauncherForActivityResult

        viewModel.onImportFullConfig(rawText) {
            reloadLocationsAfterImport {
                Toast.makeText(context, "QR импортирован", Toast.LENGTH_SHORT).show()
            }
        }
    }

    val logSaveLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.CreateDocument("text/plain")
    ) { uri: Uri? ->
        val callbacks = pendingLogSaveCallbacks.value
        pendingLogSaveCallbacks.value = null
        if (uri == null || callbacks == null) return@rememberLauncherForActivityResult

        viewModel.onSaveLogsToFile(
            target = uri,
            onSaved = callbacks.first,
            onError = callbacks.second
        )
    }

    fun navigateHomeFromLocationSettings() {
        viewModel.loadCurrentConfig()
        navigate(AppScreen.Home)
    }

    BackHandler(enabled = currentScreen is AppScreen.LocationSettings) {
        navigateHomeFromLocationSettings()
    }

    BackHandler(enabled = currentScreen is AppScreen.Subscription) {
        navigate(AppScreen.Home)
    }

    MaestroVpnAppContent(
        homeViewModel = viewModel,
        locationViewModel = locationViewModel,
        currentScreen = currentScreen,
        onNavigate = navigate,
        onToggleClick = {
            val prepIntent = if (connectionMode == AndroidConnectionMode.Tun) {
                VpnService.prepare(context)
            } else {
                null
            }
            if (prepIntent != null) {
                pendingVpnAction.value = PendingVpnPermissionAction.Toggle
                vpnRequestLauncher.launch(prepIntent)
            } else {
                viewModel.ToggleVpn()
            }
        },
        onImportFileRequested = {
            filePickerLauncher.launch("*/*")
        },
        onImportFromClipboardRequested = { onImported, onError ->
            viewModel.onPasteFromClipboard(
                onComplete = {
                    reloadLocationsAfterImport(onImported)
                },
                onError = onError
            )
        },
        onScanQrRequested = {
            qrScannerLauncher.launch(Intent(context, QrScannerActivity::class.java))
        },
        onCopyConfigRequested = {
            viewModel.onCopyFullConfigClicked()
        },
        onShareLocationRequested = { config ->
            shareSheetPayload = "QR локации" to ConfigShareService.olcRtcUri(config)
        },
        onSaveLogsRequested = { onSaved, onError ->
            pendingLogSaveCallbacks.value = onSaved to onError
            logSaveLauncher.launch(viewModel.suggestedLogsFileName())
        },
        showAppSettingsButton = true,
        showSplitTunnelingButton = false,
        canScanQr = true,
        onAppSettingsClick = {
            appSettingsInitialRoute = AppSettingsInitialRoute.Hub
            vpnManager.refreshInstalledApps()
            isAppSettingsOpen = true
        },
        onSplitTunnelingClick = {
            appSettingsInitialRoute = AppSettingsInitialRoute.SplitTunneling
            vpnManager.refreshInstalledApps()
            isAppSettingsOpen = true
        },
        paymentViewModel = paymentViewModel,
        panelUrl = panelBaseUrl,
        onPanelUrlChange = { url ->
            panelBaseUrl = url
            paymentPrefs.edit().putString("panel_url", url).apply()
        },
        onSubscriptionActivated = { token, config ->
            if (config != null && config.isConnectable()) {
                // Seed the operator's server as a ready location so the subscriber
                // can connect right away with nothing to enter by hand.
                locationViewModel.applyManagedServer(
                    name = config.name,
                    id = config.roomId,
                    key = config.key,
                    provider = config.provider,
                    transport = config.transport,
                    token = config.token.ifBlank { token }
                ) {
                    viewModel.loadCurrentConfig()
                }
                Toast.makeText(context, "Подписка активна — сервер добавлен, можно подключаться", Toast.LENGTH_LONG).show()
            } else {
                locationViewModel.applyAccessToken(token) {
                    viewModel.loadCurrentConfig()
                }
                Toast.makeText(context, "Доступ активен — токен подключён", Toast.LENGTH_LONG).show()
            }
        }
    )

    shareSheetPayload?.let { (title, payload) ->
        AndroidConfigShareSheet(
            title = title,
            payload = payload,
            onDismiss = { shareSheetPayload = null }
        )
    }

    updateOffer?.let { info ->
        ApplicationUpdateOfferSheet(
            info = info,
            downloadProgress = updateDownloadProgress,
            onLater = { postponeUpdate(info) },
            onDownload = { downloadUpdate(info) }
        )
    }

    if (isAppSettingsOpen) {
        AppSettingsSheet(
            initialRoute = appSettingsInitialRoute,
            selectedMode = connectionMode,
            proxySettings = proxySettings,
            splitTunnelSettings = splitTunnelSettings,
            installedApps = installedApps,
            logs = logs,
            dynamicThemeEnabled = dynamicThemeEnabled,
            updateSettings = updateSettings,
            updateStatusText = updateStatusText,
            updateDownloadProgress = updateDownloadProgress,
            subscriptions = subscriptionShareItems,
            enabled = !homeState.isVpnLoading,
            isConnectionActive = homeState.isVpnConnected,
            onDismiss = {
                isAppSettingsOpen = false
                applyPendingSplitTunnelRestart()
            },
            onCopyConfigClick = {
                viewModel.onCopyFullConfigClicked()
                Toast.makeText(context, "Конфигурация скопирована", Toast.LENGTH_SHORT).show()
            },
            onSaveLogsClick = {
                val showToast: (String) -> Unit = { message ->
                    Toast.makeText(context, message, Toast.LENGTH_SHORT).show()
                }
                pendingLogSaveCallbacks.value = showToast to showToast
                logSaveLauncher.launch(viewModel.suggestedLogsFileName())
            },
            onShareLogsClick = {
                val showToast: (String) -> Unit = { message ->
                    Toast.makeText(context, message, Toast.LENGTH_SHORT).show()
                }
                viewModel.onShareLogs(showToast, showToast)
            },
            onUpdateIntervalSelected = { hours ->
                scope.launch {
                    saveUpdateSettings(updateSettings.copy(intervalHours = hours))
                }
            },
            onCheckUpdatesClick = {
                checkUpdate(manual = true)
            },
            onSubscriptionShareClick = { url ->
                shareSheetPayload = "QR подписки" to ConfigShareService.subscriptionQrText(url)
            },
            onSubscriptionRefreshClick = { url ->
                viewModel.refreshSubscription(url) { updatedCount ->
                    reloadLocationsAfterImport {
                        viewModel.restartVpnIfRunning()
                        Toast.makeText(
                            context,
                            if (updatedCount > 0) "Подписка обновлена" else "Подписка не обновлена",
                            Toast.LENGTH_SHORT
                        ).show()
                    }
                }
            },
            onDynamicThemeChanged = vpnManager::setDynamicThemeEnabled,
            onModeSelected = { mode ->
                if (mode != connectionMode && homeState.isVpnConnected) {
                    val prepIntent = if (mode == AndroidConnectionMode.Tun) {
                        VpnService.prepare(context)
                    } else {
                        null
                    }
                    if (prepIntent != null) {
                        pendingVpnAction.value = PendingVpnPermissionAction.RestartWithMode(mode)
                        vpnRequestLauncher.launch(prepIntent)
                    } else {
                        vpnManager.selectConnectionMode(mode)
                        viewModel.restartVpnIfRunning()
                    }
                } else if (mode != connectionMode) {
                    vpnManager.selectConnectionMode(mode)
                }
            },
            onProxySettingsSaved = { host, username, password, port ->
                vpnManager.updateProxySettings(host, username, password, port)
                if (homeState.isVpnConnected) {
                    viewModel.restartVpnIfRunning()
                }
            },
            onProxyPasswordRegenerated = {
                vpnManager.regenerateProxyPassword()
                if (homeState.isVpnConnected) {
                    viewModel.restartVpnIfRunning()
                }
            },
            onSplitTunnelModeSelected = { mode: AndroidSplitTunnelMode ->
                vpnManager.selectSplitTunnelMode(mode)
                markSplitTunnelChanged()
            },
            onSplitTunnelAppToggled = { list: AndroidSplitTunnelList, packageName: String ->
                vpnManager.toggleSplitTunnelApp(list, packageName)
                markSplitTunnelChanged()
            },
            onSplitTunnelAppsSelected = { list: AndroidSplitTunnelList, packages: Set<String> ->
                vpnManager.setSplitTunnelApps(list, packages)
                markSplitTunnelChanged()
            },
            onSubscriptionClick = {
                isAppSettingsOpen = false
                navigate(AppScreen.Subscription)
            }
        )
    }
}

private sealed class PendingVpnPermissionAction {
    object Toggle : PendingVpnPermissionAction()
    data class RestartWithMode(val mode: AndroidConnectionMode) : PendingVpnPermissionAction()
}
