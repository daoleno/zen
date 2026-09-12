package expo.modules.zenremotedesktop.moonlight;

import java.security.PrivateKey;
import java.security.cert.X509Certificate;

/**
 * Upstream LimelightCryptoProvider (moonlight-android@98c12beb,
 * app/src/main/java/com/limelight/nvstream/http/LimelightCryptoProvider.java).
 */
public interface LimelightCryptoProvider {
    X509Certificate getClientCertificate();
    PrivateKey getClientPrivateKey();
    byte[] getPemEncodedClientCertificate();
    String encodeBase64String(byte[] data);
}
