package expo.modules.zenremotedesktop.moonlight;

import java.util.logging.Logger;

/**
 * Log shim mirroring upstream LimeLog (moonlight-android@98c12beb). Uses
 * java.util.logging instead of android.util.Log so the vendored application
 * layer is also exercised by host JVM tests without Android stubs.
 * Never logs secrets (no RI keys, pairing payloads or frame content).
 */
public final class MoonlightLog {
    private static final Logger LOGGER = Logger.getLogger("zen-moonlight");

    private MoonlightLog() {}

    public static void info(String message) {
        LOGGER.info(message);
    }

    public static void warning(String message) {
        LOGGER.warning(message);
    }

    public static void severe(String message) {
        LOGGER.severe(message);
    }
}
