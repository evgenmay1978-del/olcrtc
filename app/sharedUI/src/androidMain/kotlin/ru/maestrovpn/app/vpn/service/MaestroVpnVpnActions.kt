package ru.maestrovpn.app.vpn.service

object MaestroVpnVpnActions {
    const val SERVICE_CLASS_NAME = "ru.maestrovpn.app.vpn.service.MaestroVpnVpnService"
    const val ACTION_START_VPN = "ru.maestrovpn.app.vpn.service.MaestroVpnVpnService.START"
    const val ACTION_STOP_VPN = "ru.maestrovpn.app.vpn.service.MaestroVpnVpnService.STOP"
    const val EXTRA_CONNECTION_MODE = "ru.maestrovpn.app.vpn.service.MaestroVpnVpnService.CONNECTION_MODE"
    const val EXTRA_SOCKS_HOST = "ru.maestrovpn.app.vpn.service.MaestroVpnVpnService.SOCKS_HOST"
    const val EXTRA_SOCKS_PORT = "ru.maestrovpn.app.vpn.service.MaestroVpnVpnService.SOCKS_PORT"
    const val EXTRA_SOCKS_USERNAME = "ru.maestrovpn.app.vpn.service.MaestroVpnVpnService.SOCKS_USERNAME"
    const val EXTRA_SOCKS_PASSWORD = "ru.maestrovpn.app.vpn.service.MaestroVpnVpnService.SOCKS_PASSWORD"
    const val EXTRA_SPLIT_TUNNEL_MODE = "ru.maestrovpn.app.vpn.service.MaestroVpnVpnService.SPLIT_TUNNEL_MODE"
    const val EXTRA_SPLIT_TUNNEL_PROXY_APPS = "ru.maestrovpn.app.vpn.service.MaestroVpnVpnService.SPLIT_TUNNEL_PROXY_APPS"
    const val EXTRA_SPLIT_TUNNEL_BYPASS_APPS = "ru.maestrovpn.app.vpn.service.MaestroVpnVpnService.SPLIT_TUNNEL_BYPASS_APPS"

    // Autostart-on-boot: startVpn mirrors the last start's settings here so
    // BootReceiver can replay an identical start after a reboot. KEY_WAS_CONNECTED
    // gates it so we only reconnect when the VPN was actually running at shutdown.
    const val AUTOSTART_PREFS = "maestrovpn_autostart"
    const val KEY_AUTOSTART_ENABLED = "autostart_enabled"
    const val KEY_WAS_CONNECTED = "was_connected"
}
