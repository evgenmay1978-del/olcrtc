package ru.maestrovpn.app.vpn

import android.content.Context
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import androidx.core.content.ContextCompat
import androidx.datastore.preferences.core.edit
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit
import kotlinx.coroutines.withContext
import ru.maestrovpn.app.data.model.LocationConfig
import ru.maestrovpn.app.data.datasource.LocationsDataSourceImpl
import ru.maestrovpn.app.data.identity.PersistentDeviceIdentityProvider
import ru.maestrovpn.app.data.repository.SubscriptionFetchProxy
import ru.maestrovpn.app.vpn.data.KEY_ANDROID_CONNECTION_MODE
import ru.maestrovpn.app.vpn.data.KEY_ANDROID_DYNAMIC_THEME
import ru.maestrovpn.app.vpn.data.KEY_ANDROID_SPLIT_TUNNEL_BYPASS_APPS
import ru.maestrovpn.app.vpn.data.KEY_ANDROID_SPLIT_TUNNEL_MODE
import ru.maestrovpn.app.vpn.data.KEY_ANDROID_SPLIT_TUNNEL_PROXY_APPS
import ru.maestrovpn.app.vpn.data.KEY_ANDROID_SOCKS_HOST
import ru.maestrovpn.app.vpn.data.KEY_ANDROID_SOCKS_PASSWORD
import ru.maestrovpn.app.vpn.data.KEY_ANDROID_SOCKS_PORT
import ru.maestrovpn.app.vpn.data.KEY_ANDROID_SOCKS_USERNAME
import ru.maestrovpn.app.vpn.data.KEY_ANDROID_SOCKS_USERNAME_INITIALIZED
import ru.maestrovpn.app.vpn.data.vpnPrefDataStore
import ru.maestrovpn.app.vpn.service.MaestroVpnVpnActions
import ru.maestrovpn.app.vpn.service.MaestroVpnVpnState
import java.security.SecureRandom

class AndroidVpnManager(private val context: Context) : VpnManager {
    private val appContext = context.applicationContext
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val _connectionMode = MutableStateFlow(AndroidConnectionMode.Tun)
    private val _proxySettings = MutableStateFlow(AndroidSocksProxySettings())
    private val _splitTunnelSettings = MutableStateFlow(AndroidSplitTunnelSettings())
    // Off by default so the MaestroVPN gold/dark brand theme is what users see.
    // When on (Android 12+), Material You would derive colors from the wallpaper
    // and discard the brand scheme entirely.
    private val _dynamicThemeEnabled = MutableStateFlow(false)
    private val _installedApps = MutableStateFlow<List<AndroidInstalledApp>>(emptyList())
    private val deviceIdentityProvider = PersistentDeviceIdentityProvider(
        LocationsDataSourceImpl(appContext)
    )

    override val logs: StateFlow<List<String>> = MaestroVpnVpnState.logs
    override val status: StateFlow<VpnStatus> = MaestroVpnVpnState.status
    override val isConnected: StateFlow<Boolean> = MaestroVpnVpnState.isConnected
    val connectionMode: StateFlow<AndroidConnectionMode> = _connectionMode.asStateFlow()
    val proxySettings: StateFlow<AndroidSocksProxySettings> = _proxySettings.asStateFlow()
    val splitTunnelSettings: StateFlow<AndroidSplitTunnelSettings> = _splitTunnelSettings.asStateFlow()
    val dynamicThemeEnabled: StateFlow<Boolean> = _dynamicThemeEnabled.asStateFlow()
    val installedApps: StateFlow<List<AndroidInstalledApp>> = _installedApps.asStateFlow()

    init {
        scope.launch {
            ensureProxySettings()
            appContext.vpnPrefDataStore.data
                .map { preferences ->
                    val mode = AndroidConnectionMode.fromValue(preferences[KEY_ANDROID_CONNECTION_MODE])
                    val proxy = AndroidSocksProxySettings(
                        host = AndroidSocksProxySettings.sanitizeHost(
                            preferences[KEY_ANDROID_SOCKS_HOST]
                        ),
                        port = AndroidSocksProxySettings.sanitizePort(
                            preferences[KEY_ANDROID_SOCKS_PORT]
                        ),
                        username = preferences[KEY_ANDROID_SOCKS_USERNAME].orEmpty(),
                        password = preferences[KEY_ANDROID_SOCKS_PASSWORD].orEmpty()
                    )
                    val splitTunnel = AndroidSplitTunnelSettings(
                        mode = AndroidSplitTunnelMode.fromValue(
                            preferences[KEY_ANDROID_SPLIT_TUNNEL_MODE]
                        ),
                        proxyPackages = preferences[KEY_ANDROID_SPLIT_TUNNEL_PROXY_APPS].orEmpty(),
                        bypassPackages = preferences[KEY_ANDROID_SPLIT_TUNNEL_BYPASS_APPS].orEmpty()
                    )
                    AndroidAppPreferences(
                        mode = mode,
                        proxy = proxy,
                        splitTunnel = splitTunnel,
                        dynamicThemeEnabled = preferences[KEY_ANDROID_DYNAMIC_THEME] == true
                    )
                }
                .collect { settings ->
                    _connectionMode.value = settings.mode
                    _proxySettings.value = settings.proxy
                    _splitTunnelSettings.value = settings.splitTunnel
                    _dynamicThemeEnabled.value = settings.dynamicThemeEnabled
                }
        }
        refreshInstalledApps()
    }

    override fun needsPermission(): Boolean = needsPermission(_connectionMode.value)

    fun needsPermission(mode: AndroidConnectionMode): Boolean {
        return mode == AndroidConnectionMode.Tun && VpnService.prepare(context) != null
    }

    fun selectConnectionMode(mode: AndroidConnectionMode) {
        _connectionMode.value = mode
        scope.launch {
            appContext.vpnPrefDataStore.edit { preferences ->
                preferences[KEY_ANDROID_CONNECTION_MODE] = mode.value
            }
        }
    }

    fun setDynamicThemeEnabled(enabled: Boolean) {
        _dynamicThemeEnabled.value = enabled
        scope.launch {
            appContext.vpnPrefDataStore.edit { preferences ->
                preferences[KEY_ANDROID_DYNAMIC_THEME] = enabled
            }
        }
    }

    fun updateProxySettings(
        host: String,
        username: String,
        password: String,
        port: Int = _proxySettings.value.port
    ) {
        val sanitizedHost = AndroidSocksProxySettings.sanitizeHost(host)
        val sanitizedUsername = username.trim().take(MAX_SOCKS_USERNAME_LENGTH)
            .ifBlank { generateProxyUsername() }
        val sanitized = password.trim().take(MAX_SOCKS_PASSWORD_LENGTH)
            .ifBlank { generateProxyPassword() }
        val sanitizedPort = AndroidSocksProxySettings.sanitizePort(port)
        _proxySettings.value = _proxySettings.value.copy(
            host = sanitizedHost,
            port = sanitizedPort,
            username = sanitizedUsername,
            password = sanitized
        )
        scope.launch {
            appContext.vpnPrefDataStore.edit { preferences ->
                preferences[KEY_ANDROID_SOCKS_HOST] = sanitizedHost
                preferences[KEY_ANDROID_SOCKS_PORT] = sanitizedPort
                preferences[KEY_ANDROID_SOCKS_USERNAME] = sanitizedUsername
                preferences[KEY_ANDROID_SOCKS_USERNAME_INITIALIZED] = true
                preferences[KEY_ANDROID_SOCKS_PASSWORD] = sanitized
            }
        }
    }

    fun updateProxyPassword(password: String) {
        updateProxySettings(
            host = _proxySettings.value.host,
            username = _proxySettings.value.username,
            password = password
        )
    }

    fun regenerateProxyPassword() {
        updateProxyPassword(generateProxyPassword())
    }

    fun refreshInstalledApps() {
        scope.launch {
            _installedApps.value = loadInstalledApps()
        }
    }

    fun selectSplitTunnelMode(mode: AndroidSplitTunnelMode) {
        _splitTunnelSettings.value = _splitTunnelSettings.value.copy(mode = mode)
        scope.launch {
            appContext.vpnPrefDataStore.edit { preferences ->
                preferences[KEY_ANDROID_SPLIT_TUNNEL_MODE] = mode.value
            }
        }
    }

    fun toggleSplitTunnelApp(list: AndroidSplitTunnelList, packageName: String) {
        val current = _splitTunnelSettings.value
        val next = when (list) {
            AndroidSplitTunnelList.Proxy -> {
                val packages = current.proxyPackages.toggle(packageName)
                current.copy(proxyPackages = packages)
            }

            AndroidSplitTunnelList.Bypass -> {
                val packages = current.bypassPackages.toggle(packageName)
                current.copy(bypassPackages = packages)
            }
        }

        updateSplitTunnelSettings(next)
    }

    fun setSplitTunnelApps(list: AndroidSplitTunnelList, packages: Set<String>) {
        val normalizedPackages = packages
            .map { it.trim() }
            .filter { it.isNotBlank() }
            .toSet()
        val current = _splitTunnelSettings.value
        val next = when (list) {
            AndroidSplitTunnelList.Proxy -> current.copy(proxyPackages = normalizedPackages)
            AndroidSplitTunnelList.Bypass -> current.copy(bypassPackages = normalizedPackages)
        }

        updateSplitTunnelSettings(next)
    }

    override fun startVpn() {
        val intent = Intent().apply {
            setClassName(context.packageName, MaestroVpnVpnActions.SERVICE_CLASS_NAME)
            action = MaestroVpnVpnActions.ACTION_START_VPN
            putExtra(MaestroVpnVpnActions.EXTRA_CONNECTION_MODE, _connectionMode.value.value)
            putExtra(MaestroVpnVpnActions.EXTRA_SOCKS_HOST, _proxySettings.value.host)
            putExtra(MaestroVpnVpnActions.EXTRA_SOCKS_PORT, _proxySettings.value.port)
            putExtra(MaestroVpnVpnActions.EXTRA_SOCKS_USERNAME, _proxySettings.value.username)
            putExtra(MaestroVpnVpnActions.EXTRA_SOCKS_PASSWORD, _proxySettings.value.password)
            putExtra(MaestroVpnVpnActions.EXTRA_SPLIT_TUNNEL_MODE, _splitTunnelSettings.value.mode.value)
            putStringArrayListExtra(
                MaestroVpnVpnActions.EXTRA_SPLIT_TUNNEL_PROXY_APPS,
                ArrayList(_splitTunnelSettings.value.proxyPackages)
            )
            putStringArrayListExtra(
                MaestroVpnVpnActions.EXTRA_SPLIT_TUNNEL_BYPASS_APPS,
                ArrayList(_splitTunnelSettings.value.bypassPackages)
            )
        }
        persistAutostartState(connected = true)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            ContextCompat.startForegroundService(context, intent)
        } else {
            context.startService(intent)
        }
    }

    override fun stopVpn() {
        // A user-initiated stop means "stay off after reboot" too.
        persistAutostartState(connected = false)
        val intent = Intent().apply {
            setClassName(context.packageName, MaestroVpnVpnActions.SERVICE_CLASS_NAME)
            action = MaestroVpnVpnActions.ACTION_STOP_VPN
        }
        context.startService(intent)
    }

    // Mirror the current runtime settings (and whether the VPN should come back
    // on boot) into plain SharedPreferences so BootReceiver can replay the exact
    // same start intent without touching DataStore from a broadcast receiver.
    private fun persistAutostartState(connected: Boolean) {
        appContext
            .getSharedPreferences(MaestroVpnVpnActions.AUTOSTART_PREFS, Context.MODE_PRIVATE)
            .edit()
            .putBoolean(MaestroVpnVpnActions.KEY_WAS_CONNECTED, connected)
            .putString(MaestroVpnVpnActions.EXTRA_CONNECTION_MODE, _connectionMode.value.value)
            .putString(MaestroVpnVpnActions.EXTRA_SOCKS_HOST, _proxySettings.value.host)
            .putInt(MaestroVpnVpnActions.EXTRA_SOCKS_PORT, _proxySettings.value.port)
            .putString(MaestroVpnVpnActions.EXTRA_SOCKS_USERNAME, _proxySettings.value.username)
            .putString(MaestroVpnVpnActions.EXTRA_SOCKS_PASSWORD, _proxySettings.value.password)
            .putString(MaestroVpnVpnActions.EXTRA_SPLIT_TUNNEL_MODE, _splitTunnelSettings.value.mode.value)
            .putStringSet(
                MaestroVpnVpnActions.EXTRA_SPLIT_TUNNEL_PROXY_APPS,
                _splitTunnelSettings.value.proxyPackages
            )
            .putStringSet(
                MaestroVpnVpnActions.EXTRA_SPLIT_TUNNEL_BYPASS_APPS,
                _splitTunnelSettings.value.bypassPackages
            )
            .apply()
    }

    override suspend fun ping(locationConfig: LocationConfig): Long? {
        return OlcRtcConnectionChecker.ping(
            locationConfig = locationConfig,
            deviceId = deviceIdentityProvider.hwid()
        )
    }

    override suspend fun checkConnection(locationConfig: LocationConfig): Long? {
        return OlcRtcConnectionChecker.check(
            locationConfig = locationConfig,
            deviceId = deviceIdentityProvider.hwid()
        )
    }

    override fun subscriptionFetchProxy(): SubscriptionFetchProxy? {
        val currentStatus = status.value
        if (currentStatus !is VpnStatus.Connected &&
            currentStatus !is VpnStatus.Reconnecting
        ) {
            return null
        }

        val proxy = _proxySettings.value
        return SubscriptionFetchProxy(
            host = AndroidSocksProxySettings.connectHost(proxy.host),
            port = proxy.port,
            username = proxy.username,
            password = proxy.password
        )
    }

    private suspend fun ensureProxySettings() {
        appContext.vpnPrefDataStore.edit { preferences ->
            val username = preferences[KEY_ANDROID_SOCKS_USERNAME]
            val usernameInitialized = preferences[KEY_ANDROID_SOCKS_USERNAME_INITIALIZED] == true
            if (username.isNullOrBlank() || (!usernameInitialized && username == LEGACY_DEFAULT_USERNAME)) {
                preferences[KEY_ANDROID_SOCKS_USERNAME] = generateProxyUsername()
            }
            preferences[KEY_ANDROID_SOCKS_USERNAME_INITIALIZED] = true
            if (preferences[KEY_ANDROID_SOCKS_PASSWORD].isNullOrBlank()) {
                preferences[KEY_ANDROID_SOCKS_PASSWORD] = generateProxyPassword()
            }
            preferences[KEY_ANDROID_SOCKS_HOST] = AndroidSocksProxySettings.sanitizeHost(
                preferences[KEY_ANDROID_SOCKS_HOST]
            )
            preferences[KEY_ANDROID_SOCKS_PORT] = AndroidSocksProxySettings.sanitizePort(
                preferences[KEY_ANDROID_SOCKS_PORT]
            )
        }
    }

    private suspend fun loadInstalledApps(): List<AndroidInstalledApp> = withContext(Dispatchers.IO) {
        val packageManager = appContext.packageManager
        val launcherIntent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        val resolveInfos = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            packageManager.queryIntentActivities(
                launcherIntent,
                PackageManager.ResolveInfoFlags.of(0)
            )
        } else {
            @Suppress("DEPRECATION")
            packageManager.queryIntentActivities(launcherIntent, 0)
        }
        val launcherApps = resolveInfos
            .mapNotNull { it.activityInfo?.applicationInfo }

        val installedApps = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            packageManager.getInstalledApplications(
                PackageManager.ApplicationInfoFlags.of(0)
            )
        } else {
            @Suppress("DEPRECATION")
            packageManager.getInstalledApplications(0)
        }

        (launcherApps + installedApps)
            .filter { it.packageName != appContext.packageName }
            .distinctBy { it.packageName }
            .map { appInfo ->
                AndroidInstalledApp(
                    packageName = appInfo.packageName,
                    label = appInfo.loadLabel(packageManager).toString(),
                    isSystem = appInfo.isSystemApp()
                )
            }
            .sortedWith(compareBy<AndroidInstalledApp> { it.label.lowercase() }.thenBy { it.packageName })
    }

    private fun ApplicationInfo.isSystemApp(): Boolean {
        val systemFlags = ApplicationInfo.FLAG_SYSTEM or ApplicationInfo.FLAG_UPDATED_SYSTEM_APP
        return flags and systemFlags != 0
    }

    private fun generateProxyPassword(): String {
        return buildString(PROXY_PASSWORD_LENGTH) {
            repeat(PROXY_PASSWORD_LENGTH) {
                append(PROXY_PASSWORD_ALPHABET[random.nextInt(PROXY_PASSWORD_ALPHABET.length)])
            }
        }
    }

    private fun generateProxyUsername(): String {
        return buildString(PROXY_USERNAME_PREFIX.length + PROXY_USERNAME_RANDOM_LENGTH) {
            append(PROXY_USERNAME_PREFIX)
            repeat(PROXY_USERNAME_RANDOM_LENGTH) {
                append(PROXY_USERNAME_ALPHABET[random.nextInt(PROXY_USERNAME_ALPHABET.length)])
            }
        }
    }

    private fun Set<String>.toggle(value: String): Set<String> {
        return if (value in this) this - value else this + value
    }

    private fun updateSplitTunnelSettings(settings: AndroidSplitTunnelSettings) {
        _splitTunnelSettings.value = settings
        scope.launch {
            appContext.vpnPrefDataStore.edit { preferences ->
                preferences[KEY_ANDROID_SPLIT_TUNNEL_PROXY_APPS] = settings.proxyPackages
                preferences[KEY_ANDROID_SPLIT_TUNNEL_BYPASS_APPS] = settings.bypassPackages
            }
        }
    }

    private data class AndroidAppPreferences(
        val mode: AndroidConnectionMode,
        val proxy: AndroidSocksProxySettings,
        val splitTunnel: AndroidSplitTunnelSettings,
        val dynamicThemeEnabled: Boolean
    )

    private companion object {
        const val LEGACY_DEFAULT_USERNAME = "maestrovpn"
        const val PROXY_USERNAME_PREFIX = "maestrovpn"
        const val PROXY_USERNAME_RANDOM_LENGTH = 8
        const val MAX_SOCKS_USERNAME_LENGTH = 64
        const val PROXY_PASSWORD_LENGTH = 24
        const val MAX_SOCKS_PASSWORD_LENGTH = 64
        const val PROXY_USERNAME_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
        const val PROXY_PASSWORD_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
        const val DEFAULT_LOCATION_PING_PARALLELISM = 4
        val random = SecureRandom()
    }
}
