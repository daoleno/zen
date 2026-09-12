package expo.modules.zenremotedesktop.moonlight;

import java.io.IOException;
import java.security.cert.X509Certificate;

/** Host operations the Zen session orchestration needs. */
public interface MoonlightHost {
    String address();

    MoonlightServerInfo fetchServerInfo() throws IOException, MoonlightParseException;

    X509Certificate getServerCert();

    void setServerCert(X509Certificate serverCert);

    PairingManager getPairingManager();

    LaunchOutcome launchOrResume(MoonlightLaunchRequest request) throws IOException, MoonlightParseException;

    boolean quitApp() throws IOException, MoonlightParseException;

    void unpair() throws IOException;

    /** Cancels in-flight HTTP calls so revoke does not wait on the network. */
    void cancelInFlight();

    /**
     * The host accepted /launch or /resume. Establishment is separate: the C
     * core reports connectionStarted after the stream is actually up.
     */
    final class LaunchOutcome {
        private final boolean accepted;
        private final String rtspSessionUrl;

        public LaunchOutcome(boolean accepted, String rtspSessionUrl) {
            this.accepted = accepted;
            this.rtspSessionUrl = rtspSessionUrl;
        }

        public boolean accepted() {
            return accepted;
        }

        public String rtspSessionUrl() {
            return rtspSessionUrl;
        }
    }
}
