package ru.maestrovpn.app.data.identity

import kotlin.random.Random
import ru.maestrovpn.app.data.datasource.LocationsDataSource

interface DeviceIdentityProvider {
    suspend fun hwid(): String
}

class PersistentDeviceIdentityProvider(
    private val dataSource: LocationsDataSource
) : DeviceIdentityProvider {
    override suspend fun hwid(): String {
        dataSource.loadDeviceIdentity()?.let { return it }

        val generated = generateInstallId()
        dataSource.saveDeviceIdentity(generated)
        return generated
    }

    private fun generateInstallId(): String {
        val random = Random.Default.nextBytes(16)
        return "install-" + random.joinToString("") { byte ->
            (byte.toInt() and 0xff).toString(16).padStart(2, '0')
        }
    }
}
