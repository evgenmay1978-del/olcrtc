package ru.maestrovpn.app.data.datasource

import kotlinx.coroutines.test.runTest
import ru.maestrovpn.app.data.model.LocationBundleV4
import ru.maestrovpn.app.data.model.LocationConfig
import ru.maestrovpn.app.data.model.LocationEntry
import java.nio.file.Files
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull

class JvmLocationsDataSourceImplTest {

    @Test
    fun storesLocationBundleInProvidedDirectory() = runTest {
        val dir = Files.createTempDirectory("maestrovpn-locations-test")
        val source = JvmLocationsDataSourceImpl(dir)
        val bundle = LocationBundleV4(
            activeLocationId = "desk",
            locations = listOf(
                LocationEntry.from(
                    "desk",
                    LocationConfig("Desktop", "room", "a".repeat(64), LocationConfig.PROVIDER_WB_STREAM)
                )
            )
        )

        source.saveLocationBundle(bundle)

        val loaded = source.loadLocationBundle()
        assertNotNull(loaded)
        assertEquals("desk", loaded.activeLocationId)
        assertEquals(LocationConfig.PROVIDER_WB_STREAM, loaded.locations.first().location.bypassProvider)
    }

    @Test
    fun storesDeviceIdentityInProvidedDirectory() = runTest {
        val dir = Files.createTempDirectory("maestrovpn-device-id-test")
        val source = JvmLocationsDataSourceImpl(dir)

        source.saveDeviceIdentity("install-test")

        assertEquals("install-test", source.loadDeviceIdentity())
    }
}
