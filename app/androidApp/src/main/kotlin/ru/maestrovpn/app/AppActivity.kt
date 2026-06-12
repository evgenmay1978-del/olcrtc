package ru.maestrovpn.app

import android.Manifest
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import ru.maestrovpn.app.data.datasource.LocationsDataSourceImpl
import ru.maestrovpn.app.data.datasource.LocationsRepositoryImpl
import ru.maestrovpn.app.data.exporter.AndroidLogExporter
import ru.maestrovpn.app.data.identity.PersistentDeviceIdentityProvider
import ru.maestrovpn.app.data.importer.AndroidConfigImporter
import ru.maestrovpn.app.ui.activities.AndroidMainScreen
import ru.maestrovpn.app.ui.features.home.HomeScreenViewModel
import ru.maestrovpn.app.ui.features.locations.LocationViewModel
import ru.maestrovpn.app.ui.theme.AppTheme
import ru.maestrovpn.app.update.AppUpdateService
import ru.maestrovpn.app.vpn.AndroidVpnManager

class AppActivity : ComponentActivity() {

    private val requestPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { _ ->
        // Permission handled
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // Request notification permission for Android 13+
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            requestPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
        }

        val vpnManager = AndroidVpnManager(this)
        val locationsDataSource = LocationsDataSourceImpl(this)
        val locationsRepository = LocationsRepositoryImpl(locationsDataSource)
        val configImporter = AndroidConfigImporter(this)
        val logExporter = AndroidLogExporter(this)
        val updateService = AppUpdateService(
            deviceIdentityProvider = PersistentDeviceIdentityProvider(locationsDataSource)
        )

        val viewModel = HomeScreenViewModel(
            vpnManager = vpnManager,
            locationsRepository = locationsRepository,
            configImporter = configImporter,
            logExporter = logExporter
        )
        val locationViewModel = LocationViewModel(
            locationsRepository = locationsRepository
        )

        enableEdgeToEdge()
        setContent {
            val dynamicThemeEnabled by vpnManager.dynamicThemeEnabled.collectAsState()

            AppTheme(useDynamicColor = dynamicThemeEnabled) {
                AndroidMainScreen(
                    viewModel = viewModel,
                    locationViewModel = locationViewModel,
                    vpnManager = vpnManager,
                    appUpdateService = updateService
                )
            }
        }
    }
}
