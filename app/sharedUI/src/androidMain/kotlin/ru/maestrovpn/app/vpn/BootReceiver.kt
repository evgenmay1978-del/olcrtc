package ru.maestrovpn.app.vpn

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.Build
import androidx.core.content.ContextCompat
import ru.maestrovpn.app.vpn.service.MaestroVpnVpnActions

/**
 * Reconnects the VPN after a reboot (or an app update) when it was running at
 * shutdown. The settings of the last successful start are mirrored into plain
 * SharedPreferences by [AndroidVpnManager.startVpn], so here we just replay that
 * exact start intent.
 *
 * Two hard constraints shape this:
 *  - The system VPN-consent dialog cannot be shown from a broadcast receiver, so
 *    we only proceed when consent was already granted (`VpnService.prepare` ==
 *    null). The user grants it once by connecting manually; it then persists.
 *  - Starting a foreground service from the background is restricted on newer
 *    Android, so the start is wrapped in runCatching to fail quietly instead of
 *    crashing at boot.
 */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        when (intent.action) {
            Intent.ACTION_BOOT_COMPLETED,
            Intent.ACTION_MY_PACKAGE_REPLACED,
            "android.intent.action.QUICKBOOT_POWERON" -> Unit
            else -> return
        }

        val prefs = context.getSharedPreferences(
            MaestroVpnVpnActions.AUTOSTART_PREFS,
            Context.MODE_PRIVATE
        )
        if (!prefs.getBoolean(MaestroVpnVpnActions.KEY_AUTOSTART_ENABLED, true)) return
        if (!prefs.getBoolean(MaestroVpnVpnActions.KEY_WAS_CONNECTED, false)) return
        if (VpnService.prepare(context) != null) return

        val service = Intent().apply {
            setClassName(context.packageName, MaestroVpnVpnActions.SERVICE_CLASS_NAME)
            action = MaestroVpnVpnActions.ACTION_START_VPN
            prefs.getString(MaestroVpnVpnActions.EXTRA_CONNECTION_MODE, null)?.let {
                putExtra(MaestroVpnVpnActions.EXTRA_CONNECTION_MODE, it)
            }
            putExtra(
                MaestroVpnVpnActions.EXTRA_SOCKS_HOST,
                prefs.getString(MaestroVpnVpnActions.EXTRA_SOCKS_HOST, "")
            )
            putExtra(
                MaestroVpnVpnActions.EXTRA_SOCKS_PORT,
                prefs.getInt(MaestroVpnVpnActions.EXTRA_SOCKS_PORT, 0)
            )
            putExtra(
                MaestroVpnVpnActions.EXTRA_SOCKS_USERNAME,
                prefs.getString(MaestroVpnVpnActions.EXTRA_SOCKS_USERNAME, "")
            )
            putExtra(
                MaestroVpnVpnActions.EXTRA_SOCKS_PASSWORD,
                prefs.getString(MaestroVpnVpnActions.EXTRA_SOCKS_PASSWORD, "")
            )
            prefs.getString(MaestroVpnVpnActions.EXTRA_SPLIT_TUNNEL_MODE, null)?.let {
                putExtra(MaestroVpnVpnActions.EXTRA_SPLIT_TUNNEL_MODE, it)
            }
            putStringArrayListExtra(
                MaestroVpnVpnActions.EXTRA_SPLIT_TUNNEL_PROXY_APPS,
                ArrayList(
                    prefs.getStringSet(MaestroVpnVpnActions.EXTRA_SPLIT_TUNNEL_PROXY_APPS, emptySet())
                        ?: emptySet()
                )
            )
            putStringArrayListExtra(
                MaestroVpnVpnActions.EXTRA_SPLIT_TUNNEL_BYPASS_APPS,
                ArrayList(
                    prefs.getStringSet(MaestroVpnVpnActions.EXTRA_SPLIT_TUNNEL_BYPASS_APPS, emptySet())
                        ?: emptySet()
                )
            )
        }

        runCatching {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                ContextCompat.startForegroundService(context, service)
            } else {
                context.startService(service)
            }
        }
    }
}
