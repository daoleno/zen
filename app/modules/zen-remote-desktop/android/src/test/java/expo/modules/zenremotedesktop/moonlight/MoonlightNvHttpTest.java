package expo.modules.zenremotedesktop.moonlight;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertThrows;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

import java.io.File;
import java.nio.file.Files;
import java.security.cert.CertificateException;
import java.security.cert.X509Certificate;

import javax.net.ssl.X509TrustManager;

public class MoonlightNvHttpTest {

    private static final String SERVER_INFO = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>"
            + "<root status_code=\"200\">"
            + "<hostname>zen-sunshine</hostname>"
            + "<appversion>7.1.431.0</appversion>"
            + "<GfeVersion>Sunshine/0.23.1</GfeVersion>"
            + "<HttpsPort>47984</HttpsPort>"
            + "<ExternalPort>47989</ExternalPort>"
            + "<ServerCodecModeSupport>256</ServerCodecModeSupport>"
            + "<MaxLumaPixelsH264>6220800</MaxLumaPixelsH264>"
            + "<MaxLumaPixelsHEVC>8294400</MaxLumaPixelsHEVC>"
            + "<gputype>AMD Radeon</gputype>"
            + "<PairStatus>1</PairStatus>"
            + "<currentgame>0</currentgame>"
            + "<state>IDLE</state>"
            + "</root>";

    private static ZenCryptoProvider provider(File dir) {
        return new ZenCryptoProvider(dir);
    }

    @Test
    public void serverInfoParsesIntoActualTypedFields() throws Exception {
        MoonlightServerInfo info = MoonlightServerInfo.parse(SERVER_INFO);
        assertEquals("zen-sunshine", info.hostname());
        assertEquals("7.1.431.0", info.appVersion());
        assertEquals("Sunshine/0.23.1", info.gfeVersion());
        assertEquals(47984, info.httpsPort());
        assertEquals(47989, info.externalPort());
        assertEquals(256L, info.serverCodecModeSupport());
        assertEquals(6220800L, info.maxLumaPixelsH264());
        assertEquals(8294400L, info.maxLumaPixelsHEVC());
        assertEquals("AMD Radeon", info.gpuType());
        assertTrue(info.paired());
        assertEquals(0, info.runningGameId());
        assertEquals(7, info.majorVersion());
    }

    @Test
    public void pinnedCertificateTrustManagerAcceptsOnlyTheExactPin() throws Exception {
        File clientDir = Files.createTempDirectory("zen-client").toFile();
        File serverDir = Files.createTempDirectory("zen-server").toFile();
        File otherServerDir = Files.createTempDirectory("zen-other").toFile();
        X509Certificate clientCert = provider(clientDir).getClientCertificate();
        X509Certificate serverCert = provider(serverDir).getClientCertificate();
        X509Certificate otherCert = provider(otherServerDir).getClientCertificate();

        MoonlightNvHttp http = new MoonlightNvHttp("192.0.2.10", 47989, 47984,
                "ZENUNIQUEID00001", "zen", serverCert, provider(clientDir));
        X509TrustManager trustManager = http.trustManagerForTesting();

        // The exact pinned self-signed certificate is accepted.
        trustManager.checkServerTrusted(new X509Certificate[]{serverCert}, "RSA");

        // A different self-signed certificate is rejected.
        assertThrows(CertificateException.class,
                () -> trustManager.checkServerTrusted(new X509Certificate[]{otherCert}, "RSA"));

        // A chain from an unpinned root is not silently accepted.
        assertThrows(CertificateException.class,
                () -> trustManager.checkServerTrusted(new X509Certificate[]{otherCert, serverCert}, "RSA"));

        // Client authentication is never accepted by this trust manager.
        assertThrows(IllegalStateException.class,
                () -> trustManager.checkClientTrusted(new X509Certificate[]{clientCert}, "RSA"));
    }

    @Test
    public void launchQueryUsesTypedParametersAndNeverZeroesKeys() {
        byte[] riKey = new byte[16];
        for (int i = 0; i < riKey.length; i++) {
            riKey[i] = (byte) (i + 1);
        }
        MoonlightLaunchRequest request = new MoonlightLaunchRequest.Builder()
                .verb(MoonlightLaunchRequest.VERB_LAUNCH)
                .appId(1)
                .resolution(1920, 1080, 60)
                .riKey(riKey, 42)
                .audio(0x302CA, false)
                .controllers(0, 0, false)
                .sops(true)
                .build();
        String query = request.toQuery();
        assertTrue(query.contains("appid=1"));
        assertTrue(query.contains("mode=1920x1080x60"));
        assertTrue(query.contains("rikey=0102030405060708090A0B0C0D0E0F10"));
        assertTrue(query.contains("rikeyid=42"));
        assertTrue(query.contains("surroundAudioInfo=197322"));
        assertTrue(query.contains("&corever=1"));
        assertFalse(query.contains("hdrMode=1"));

        MoonlightLaunchRequest hdr = new MoonlightLaunchRequest.Builder()
                .appId(2)
                .resolution(3840, 2160, 60)
                .riKey(riKey, 0)
                .hdr(true)
                .build();
        assertTrue(hdr.toQuery().contains("&hdrMode=1"));

        assertThrows(IllegalArgumentException.class,
                () -> new MoonlightLaunchRequest.Builder().appId(1).resolution(1920, 1080, 60).build());
        assertThrows(IllegalArgumentException.class,
                () -> new MoonlightLaunchRequest.Builder().appId(1).resolution(0, 1080, 60)
                        .riKey(riKey, 0).build());
    }

    @Test
    public void serverInfoRequiresPairStatusAndVersion() {
        assertThrows(MoonlightParseException.class,
                () -> MoonlightServerInfo.parse("<root status_code=\"200\"><hostname>x</hostname></root>"));
        assertThrows(HostHttpResponseException.class,
                () -> MoonlightServerInfo.parse("<root status_code=\"401\"></root>"));
    }
}
