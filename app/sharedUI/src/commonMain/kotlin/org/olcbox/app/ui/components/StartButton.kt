package org.olcbox.app.ui.components

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
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
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import multiplatform_app.sharedui.generated.resources.Res
import multiplatform_app.sharedui.generated.resources.eagle
import multiplatform_app.sharedui.generated.resources.fox
import org.jetbrains.compose.resources.painterResource
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.sin

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

    // The eagle circles the button while connecting/connected.
    val orbit = rememberInfiniteTransition(label = "orbit")
    val angle by orbit.animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 7000, easing = LinearEasing),
            repeatMode = RepeatMode.Restart
        ),
        label = "angle"
    )
    val showAnimals = isActive || isLoading
    // The fox peeks up from the bottom of the button when connected.
    val foxOffset by animateDpAsState(
        targetValue = if (isActive) 24.dp else 130.dp,
        animationSpec = tween(durationMillis = 650),
        label = "foxPeek"
    )

    Box(
        modifier = modifier.size(280.dp),
        contentAlignment = Alignment.Center
    ) {
        // Orbiting eagle (behind the button).
        if (showAnimals) {
            Image(
                painter = painterResource(Res.drawable.eagle),
                contentDescription = null,
                modifier = Modifier
                    .size(64.dp)
                    .offset {
                        val rad = angle * PI.toFloat() / 180f
                        val r = 122.dp.toPx()
                        IntOffset((r * cos(rad)).toInt(), (r * sin(rad)).toInt())
                    }
                    .graphicsLayer { rotationZ = angle + 90f }
            )
        }

        Box(
            modifier = Modifier
                .size(200.dp)
                .background(
                    color = when {
                        isActive -> MaterialTheme.colorScheme.secondaryContainer
                        else -> MaterialTheme.colorScheme.surfaceContainer
                    },
                    shape = CircleShape
                )
                .padding(8.dp)
                .background(color = MaterialTheme.colorScheme.surface, shape = CircleShape)
                .padding(6.dp)
                .clip(CircleShape)
                .background(color = mainButtonColor)
                .clickable(enabled = enabled) { onClick() },
            contentAlignment = Alignment.Center
        ) {
            // Fox peeking from the bottom (clipped to the circle).
            if (isActive) {
                Image(
                    painter = painterResource(Res.drawable.fox),
                    contentDescription = null,
                    modifier = Modifier
                        .size(96.dp)
                        .align(Alignment.BottomCenter)
                        .offset(y = foxOffset)
                )
            }

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
                        isLoading -> "СТОП"
                        isActive -> "СТОП"
                        requiresSetup -> "SETUP"
                        else -> "ПУСК"
                    },
                    color = contentColor.copy(alpha = if (!enabled) 0.7f else 1f),
                    fontSize = 22.sp,
                    fontWeight = FontWeight.Medium
                )
            }
        }
    }
}
