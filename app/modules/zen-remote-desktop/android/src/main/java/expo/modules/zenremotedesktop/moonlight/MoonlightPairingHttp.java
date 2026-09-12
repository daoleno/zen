package expo.modules.zenremotedesktop.moonlight;

import java.io.IOException;
import java.security.cert.X509Certificate;

/** Subset of upstream NvHTTP used by the pairing state machine. */
public interface MoonlightPairingHttp {
    int getServerMajorVersion(String serverInfo) throws MoonlightParseException, IOException;

    String executePairingCommand(String additionalArguments, boolean enableReadTimeout)
            throws HostHttpResponseException, IOException;

    String executePairingChallenge() throws HostHttpResponseException, IOException;

    void unpair() throws IOException;

    void setServerCert(X509Certificate serverCert);
}
