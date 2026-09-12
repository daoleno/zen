package expo.modules.zenremotedesktop.moonlight;

/**
 * Typed /launch or /resume parameters. Query construction mirrors upstream
 * NvHTTP.launchApp (moonlight-android@98c12beb). The RI key is held as bytes and
 * hex-encoded here; it is never logged.
 *
 * "accepted" is the host's launch/resume acknowledgement, not an established
 * stream: establishment is the C core's connectionStarted callback.
 */
public final class MoonlightLaunchRequest {
    public static final String VERB_LAUNCH = "launch";
    public static final String VERB_RESUME = "resume";

    private final String verb;
    private final int appId;
    private final int width;
    private final int height;
    private final int fps;
    private final byte[] riKey;
    private final int riKeyId;
    private final int surroundAudioInfo;
    private final boolean localAudioPlayMode;
    private final int remoteControllersBitmap;
    private final int attachedGamepadMask;
    private final boolean persistGamepads;
    private final boolean enableSops;
    private final boolean enableHdr;

    private MoonlightLaunchRequest(Builder builder) {
        this.verb = builder.verb;
        this.appId = builder.appId;
        this.width = builder.width;
        this.height = builder.height;
        this.fps = builder.fps;
        this.riKey = builder.riKey.clone();
        this.riKeyId = builder.riKeyId;
        this.surroundAudioInfo = builder.surroundAudioInfo;
        this.localAudioPlayMode = builder.localAudioPlayMode;
        this.remoteControllersBitmap = builder.remoteControllersBitmap;
        this.attachedGamepadMask = builder.attachedGamepadMask;
        this.persistGamepads = builder.persistGamepads;
        this.enableSops = builder.enableSops;
        this.enableHdr = builder.enableHdr;
    }

    public String verb() {
        return verb;
    }

    public int appId() {
        return appId;
    }

    /** Upstream-compatible query string; callers must not log it verbatim. */
    public String toQuery() {
        StringBuilder query = new StringBuilder();
        query.append("appid=").append(appId);
        query.append("&mode=").append(width).append('x').append(height).append('x').append(fps);
        query.append("&additionalStates=1&sops=").append(enableSops ? 1 : 0);
        query.append("&rikey=").append(bytesToHex(riKey));
        query.append("&rikeyid=").append(riKeyId);
        if (enableHdr) {
            query.append("&hdrMode=1&clientHdrCapVersion=0&clientHdrCapSupportedFlagsInUint32=0")
                    .append("&clientHdrCapMetaDataId=NV_STATIC_METADATA_TYPE_1");
            query.append("&clientHdrCapDisplayData=0x0x0x0x0x0x0x0x0x0x0");
        }
        query.append("&localAudioPlayMode=").append(localAudioPlayMode ? 1 : 0);
        query.append("&surroundAudioInfo=").append(surroundAudioInfo);
        query.append("&remoteControllersBitmap=").append(remoteControllersBitmap);
        query.append("&gcmap=").append(attachedGamepadMask);
        query.append("&gcpersist=").append(persistGamepads ? 1 : 0);
        // Same extended-functionality suffix as upstream MoonBridge.getLaunchUrlQueryParameters().
        query.append("&corever=1");
        return query.toString();
    }

    private static final char[] HEX = "0123456789ABCDEF".toCharArray();

    private static String bytesToHex(byte[] bytes) {
        char[] out = new char[bytes.length * 2];
        for (int i = 0; i < bytes.length; i++) {
            int value = bytes[i] & 0xFF;
            out[i * 2] = HEX[value >>> 4];
            out[i * 2 + 1] = HEX[value & 0x0F];
        }
        return new String(out);
    }

    public static final class Builder {
        private String verb = VERB_LAUNCH;
        private int appId;
        private int width;
        private int height;
        private int fps;
        private byte[] riKey;
        private int riKeyId;
        private int surroundAudioInfo;
        private boolean localAudioPlayMode;
        private int remoteControllersBitmap;
        private int attachedGamepadMask;
        private boolean persistGamepads;
        private boolean enableSops;
        private boolean enableHdr;

        public Builder verb(String verb) {
            this.verb = verb;
            return this;
        }

        public Builder appId(int appId) {
            this.appId = appId;
            return this;
        }

        public Builder resolution(int width, int height, int fps) {
            this.width = width;
            this.height = height;
            this.fps = fps;
            return this;
        }

        public Builder riKey(byte[] riKey, int riKeyId) {
            this.riKey = riKey;
            this.riKeyId = riKeyId;
            return this;
        }

        public Builder audio(int surroundAudioInfo, boolean localAudioPlayMode) {
            this.surroundAudioInfo = surroundAudioInfo;
            this.localAudioPlayMode = localAudioPlayMode;
            return this;
        }

        public Builder controllers(int remoteControllersBitmap, int attachedGamepadMask, boolean persistGamepads) {
            this.remoteControllersBitmap = remoteControllersBitmap;
            this.attachedGamepadMask = attachedGamepadMask;
            this.persistGamepads = persistGamepads;
            return this;
        }

        public Builder sops(boolean enableSops) {
            this.enableSops = enableSops;
            return this;
        }

        public Builder hdr(boolean enableHdr) {
            this.enableHdr = enableHdr;
            return this;
        }

        public MoonlightLaunchRequest build() {
            if (riKey == null || riKey.length != 16) {
                throw new IllegalArgumentException("riKey must be 16 bytes");
            }
            if (width <= 0 || height <= 0 || fps <= 0) {
                throw new IllegalArgumentException("invalid resolution or fps");
            }
            return new MoonlightLaunchRequest(this);
        }
    }
}
