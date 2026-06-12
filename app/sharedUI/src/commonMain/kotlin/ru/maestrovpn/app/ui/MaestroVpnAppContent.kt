package ru.maestrovpn.app.ui

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.ContentTransform
import androidx.compose.animation.SizeTransform
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.rememberScrollState
import androidx.compose.runtime.Composable
import ru.maestrovpn.app.data.model.LocationConfig
import ru.maestrovpn.app.ui.features.home.HomeScreen
import ru.maestrovpn.app.ui.features.home.HomeScreenViewModel
import ru.maestrovpn.app.ui.features.locations.LocationSettingsScreen
import ru.maestrovpn.app.ui.features.locations.LocationViewModel
import ru.maestrovpn.app.ui.features.payment.PaymentScreen
import ru.maestrovpn.app.ui.features.payment.PaymentViewModel
import ru.maestrovpn.app.ui.navigation.AppScreen

@Composable
fun MaestroVpnAppContent(
    homeViewModel: HomeScreenViewModel,
    locationViewModel: LocationViewModel,
    currentScreen: AppScreen,
    onNavigate: (AppScreen) -> Unit,
    onToggleClick: () -> Unit,
    onImportFileRequested: () -> Unit,
    onImportFromClipboardRequested: (onImported: () -> Unit, onError: (String) -> Unit) -> Unit,
    onScanQrRequested: () -> Unit = {},
    onCopyConfigRequested: () -> Unit,
    onShareLocationRequested: (LocationConfig) -> Unit = {},
    onSaveLogsRequested: (onSaved: (String) -> Unit, onError: (String) -> Unit) -> Unit,
    showAppSettingsButton: Boolean,
    showSplitTunnelingButton: Boolean = false,
    canScanQr: Boolean = false,
    onAppSettingsClick: () -> Unit,
    onSplitTunnelingClick: () -> Unit = {},
    paymentViewModel: PaymentViewModel? = null,
    panelUrl: String = "",
    onPanelUrlChange: (String) -> Unit = {},
    onSubscriptionActivated: (token: String) -> Unit = {}
) {
    val homeScrollState = rememberScrollState()

    AnimatedContent(
        targetState = currentScreen,
        label = "app_screen_transition",
        transitionSpec = {
            ContentTransform(
                targetContentEnter = fadeIn(
                    animationSpec = tween(
                        durationMillis = 240,
                        delayMillis = 30,
                        easing = LinearOutSlowInEasing
                    )
                ),
                initialContentExit = fadeOut(
                    animationSpec = tween(
                        durationMillis = 160,
                        easing = LinearOutSlowInEasing
                    )
                ),
                sizeTransform = SizeTransform(
                    clip = false,
                    sizeAnimationSpec = { _, _ ->
                        tween(
                            durationMillis = 420,
                            easing = FastOutSlowInEasing
                        )
                    }
                )
            )
        }
    ) { screen ->
        when (screen) {
            AppScreen.Home -> {
                HomeScreen(
                    viewModel = homeViewModel,
                    locationViewModel = locationViewModel,
                    scrollState = homeScrollState,
                    onToggleClick = onToggleClick,
                    onImportFileRequested = onImportFileRequested,
                    onImportFromClipboardRequested = onImportFromClipboardRequested,
                    onScanQrRequested = onScanQrRequested,
                    onCopyConfigRequested = onCopyConfigRequested,
                    onSaveLogsRequested = onSaveLogsRequested,
                    showAppSettingsButton = showAppSettingsButton,
                    showSplitTunnelingButton = showSplitTunnelingButton,
                    canScanQr = canScanQr,
                    onAppSettingsClick = onAppSettingsClick,
                    onSplitTunnelingClick = onSplitTunnelingClick,
                    onOpenLocationSettings = { id ->
                        locationViewModel.startEditing(id)
                        onNavigate(AppScreen.LocationSettings(id))
                    },
                    onAddLocation = {
                        locationViewModel.startEditing(null)
                        onNavigate(AppScreen.LocationSettings(null))
                    }
                )
            }

            is AppScreen.LocationSettings -> {
                LocationSettingsScreen(
                    viewModel = locationViewModel,
                    homeViewModel = homeViewModel,
                    onShareLocationRequested = onShareLocationRequested,
                    onBack = {
                        homeViewModel.loadCurrentConfig()
                        onNavigate(AppScreen.Home)
                    }
                )
            }

            AppScreen.Subscription -> {
                PaymentScreen(
                    viewModel = paymentViewModel,
                    panelUrl = panelUrl,
                    onPanelUrlChange = onPanelUrlChange,
                    onBack = { onNavigate(AppScreen.Home) },
                    onActivated = onSubscriptionActivated
                )
            }
        }
    }
}
