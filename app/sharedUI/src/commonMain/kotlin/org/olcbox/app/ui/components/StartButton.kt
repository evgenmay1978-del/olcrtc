package org.olcbox.app.ui.components

import androidx.compose.animation.animateColorAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.PowerSettingsNew
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

sealed class StartButtonState {
    object Idle : StartButtonState()
    object Loading : StartButtonState()
    object Success : StartButtonState()
}

@Composable
fun StartButton(
    modifier: Modifier = Modifier,
    isActive: Boolean,
    isLoading: Boolean,
    requiresSetup: Boolean = false,
    label: String? = null,
    enabled: Boolean = true,
    onClick: () -> Unit
) {
    val mainButtonColor by animateColorAsState(
        targetValue = when {
            isActive -> MaterialTheme.colorScheme.primary
            requiresSetup -> MaterialTheme.colorScheme.primaryContainer
            else -> MaterialTheme.colorScheme.primaryContainer
        },
        label = "buttonColor"
    )

    val contentColor = when {
        isActive -> MaterialTheme.colorScheme.onPrimary
        requiresSetup -> MaterialTheme.colorScheme.onPrimaryContainer
        else -> MaterialTheme.colorScheme.onPrimaryContainer
    }

    Box(
        modifier = modifier
            .size(200.dp)
            .background(
                color = when {
                    isActive -> MaterialTheme.colorScheme.secondaryContainer
                    requiresSetup -> MaterialTheme.colorScheme.surfaceContainer
                    else -> MaterialTheme.colorScheme.surfaceContainer
                },
                shape = CircleShape
            )
            .padding(8.dp)
            .background(color = MaterialTheme.colorScheme.surface, shape = CircleShape)
            .padding(6.dp)
            .clip(CircleShape)
            .background(color = mainButtonColor)
            .clickable(enabled = enabled) {
                onClick()
            },
        contentAlignment = Alignment.Center
    ) {
        if (isLoading) {
            CircularProgressIndicator(
                modifier = Modifier.size(176.dp),
                color = contentColor,
                strokeWidth = 4.dp
            )
        }

        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center
        ) {
            Icon(
                imageVector = Icons.Rounded.PowerSettingsNew,
                contentDescription = "Start Icon",
                tint = contentColor.copy(alpha = if (isLoading || !enabled) 0.5f else 1f),
                modifier = Modifier.size(48.dp)
            )

            Spacer(modifier = Modifier.height(8.dp))

            Text(
                text = label ?: when {
                    isLoading -> "STOP"
                    isActive -> "STOP"
                    requiresSetup -> "SETUP"
                    else -> "START"
                },
                color = contentColor.copy(alpha = if (!enabled) 0.7f else 1f),
                fontSize = 22.sp,
                fontWeight = FontWeight.Medium
            )
        }
    }
}
