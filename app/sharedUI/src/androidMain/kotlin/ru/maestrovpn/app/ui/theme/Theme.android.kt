package ru.maestrovpn.app.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ProvideTextStyle
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext

@Composable
actual fun AppTheme(
    useDynamicColor: Boolean,
    content: @Composable () -> Unit
) {
    // MaestroVPN ships a single brutal gold-on-dark identity; force the dark
    // brand scheme regardless of the phone's light/dark setting so the look is
    // consistent (and never falls back to the muted light palette).
    @Suppress("UNUSED_VARIABLE")
    val systemIsDark = isSystemInDarkTheme()
    val isDarkState = remember { mutableStateOf(true) }
    val typography = getAppTypography()

    CompositionLocalProvider(
        LocalThemeIsDark provides isDarkState
    ) {
        val isDark by isDarkState
        val colorScheme = when {
            useDynamicColor && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S -> {
                val context = LocalContext.current
                if (isDark) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
            }

            isDark -> MaestroVpnDarkColorScheme
            else -> MaestroVpnLightColorScheme
        }

        MaterialTheme(
            colorScheme = colorScheme,
            typography = typography
        ) {
            ProvideTextStyle(MaterialTheme.typography.bodyMedium, content)
        }
    }
}
