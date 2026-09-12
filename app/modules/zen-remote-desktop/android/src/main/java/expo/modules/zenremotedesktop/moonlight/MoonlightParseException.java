package expo.modules.zenremotedesktop.moonlight;

/** Thrown when the host returns XML without a mandatory field. */
public class MoonlightParseException extends Exception {
    private static final long serialVersionUID = 1L;

    public MoonlightParseException(String message) {
        super(message);
    }

    public MoonlightParseException(Throwable cause) {
        super(cause);
    }
}
