package expo.modules.zenremotedesktop.moonlight;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

import java.io.File;
import java.nio.file.Files;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.security.interfaces.RSAPrivateKey;
import java.util.Base64;

public class ZenCryptoProviderTest {

    @Test
    public void generatesPersistentSelfSignedIdentity() throws Exception {
        File identityDir = Files.createTempDirectory("zen-identity").toFile();
        ZenCryptoProvider provider = new ZenCryptoProvider(identityDir);

        X509Certificate cert = provider.getClientCertificate();
        assertNotNull(cert);
        assertEquals("RSA", cert.getPublicKey().getAlgorithm());
        assertEquals(2048, ((java.security.interfaces.RSAPublicKey) cert.getPublicKey()).getModulus().bitLength());
        assertTrue(((RSAPrivateKey) provider.getClientPrivateKey()).getModulus().bitLength() >= 2048);

        // Self-signed and verifiable with its own key.
        cert.verify(cert.getPublicKey());

        byte[] pem = provider.getPemEncodedClientCertificate();
        String pemText = new String(pem, java.nio.charset.StandardCharsets.US_ASCII);
        assertTrue(pemText.startsWith("-----BEGIN CERTIFICATE-----"));
        assertTrue(pemText.endsWith("-----END CERTIFICATE-----\n"));

        // PEM bytes parse back into the same certificate.
        X509Certificate reparsed = (X509Certificate) CertificateFactory.getInstance("X.509")
                .generateCertificate(new java.io.ByteArrayInputStream(pem));
        assertEquals(cert, reparsed);

        // A new provider over the same directory must load the same identity.
        ZenCryptoProvider reloaded = new ZenCryptoProvider(identityDir);
        assertEquals(cert, reloaded.getClientCertificate());
        assertArrayEquals(pem, reloaded.getPemEncodedClientCertificate());
        assertArrayEquals(provider.getClientPrivateKey().getEncoded(),
                reloaded.getClientPrivateKey().getEncoded());

        byte[] data = new byte[]{1, 2, 3, 4, 5};
        assertArrayEquals(Base64.getEncoder().encode(data),
                provider.encodeBase64String(data).getBytes(java.nio.charset.StandardCharsets.US_ASCII));
    }
}
