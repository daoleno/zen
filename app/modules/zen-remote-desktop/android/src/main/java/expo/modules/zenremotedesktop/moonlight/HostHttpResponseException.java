package expo.modules.zenremotedesktop.moonlight;

import java.io.IOException;

/** Upstream HostHttpResponseException (moonlight-android@98c12beb). */
public class HostHttpResponseException extends IOException {
    private static final long serialVersionUID = 1543508830807804222L;

    private final int errorCode;
    private final String errorMsg;

    public HostHttpResponseException(int errorCode, String errorMsg) {
        this.errorCode = errorCode;
        this.errorMsg = errorMsg;
    }

    public int getErrorCode() {
        return errorCode;
    }

    public String getErrorMessage() {
        return errorMsg;
    }

    @Override
    public String getMessage() {
        return "Host PC returned error: " + errorMsg + " (Error code: " + errorCode + ")";
    }
}
