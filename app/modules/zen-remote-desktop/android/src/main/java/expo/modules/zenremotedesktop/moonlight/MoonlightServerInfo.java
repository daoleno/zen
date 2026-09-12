package expo.modules.zenremotedesktop.moonlight;

/**
 * Typed host data parsed from the upstream /serverinfo response. Field names
 * follow upstream NvHTTP.getComputerDetails (moonlight-android@98c12beb).
 */
public final class MoonlightServerInfo {
    private final String hostname;
    private final String appVersion;
    private final String gfeVersion;
    private final int httpsPort;
    private final int externalPort;
    private final long serverCodecModeSupport;
    private final long maxLumaPixelsH264;
    private final long maxLumaPixelsHEVC;
    private final String gpuType;
    private final boolean paired;
    private final int runningGameId;
    private final String rawXml;

    private MoonlightServerInfo(String hostname, String appVersion, String gfeVersion, int httpsPort,
                                int externalPort, long serverCodecModeSupport, long maxLumaPixelsH264,
                                long maxLumaPixelsHEVC, String gpuType, boolean paired, int runningGameId,
                                String rawXml) {
        this.hostname = hostname;
        this.appVersion = appVersion;
        this.gfeVersion = gfeVersion;
        this.httpsPort = httpsPort;
        this.externalPort = externalPort;
        this.serverCodecModeSupport = serverCodecModeSupport;
        this.maxLumaPixelsH264 = maxLumaPixelsH264;
        this.maxLumaPixelsHEVC = maxLumaPixelsHEVC;
        this.gpuType = gpuType;
        this.paired = paired;
        this.runningGameId = runningGameId;
        this.rawXml = rawXml;
    }

    public static MoonlightServerInfo parse(String xml) throws MoonlightParseException, HostHttpResponseException {
        String appVersion = MoonlightXml.getXmlString(xml, "appversion", true);
        String hostname = MoonlightXml.getXmlString(xml, "hostname", false);
        String gfeVersion = MoonlightXml.getXmlString(xml, "GfeVersion", false);
        String httpsPortText = MoonlightXml.getXmlString(xml, "HttpsPort", false);
        String externalPortText = MoonlightXml.getXmlString(xml, "ExternalPort", false);
        String codecText = MoonlightXml.getXmlString(xml, "ServerCodecModeSupport", false);
        String lumaH264Text = MoonlightXml.getXmlString(xml, "MaxLumaPixelsH264", false);
        String lumaHevcText = MoonlightXml.getXmlString(xml, "MaxLumaPixelsHEVC", false);
        String gpuType = MoonlightXml.getXmlString(xml, "gputype", false);
        String pairStatus = MoonlightXml.getXmlString(xml, "PairStatus", true);
        String currentGame = MoonlightXml.getXmlString(xml, "currentgame", false);
        return new MoonlightServerInfo(
                hostname,
                appVersion,
                gfeVersion,
                parseInt(httpsPortText, 0),
                parseInt(externalPortText, 0),
                parseLong(codecText, 0L),
                parseLong(lumaH264Text, 0L),
                parseLong(lumaHevcText, 0L),
                gpuType,
                "1".equals(pairStatus),
                parseInt(currentGame, 0),
                xml);
    }

    private static int parseInt(String value, int fallback) {
        if (value == null) {
            return fallback;
        }
        try {
            return Integer.parseInt(value.trim());
        } catch (NumberFormatException e) {
            return fallback;
        }
    }

    private static long parseLong(String value, long fallback) {
        if (value == null) {
            return fallback;
        }
        try {
            return Long.parseLong(value.trim());
        } catch (NumberFormatException e) {
            return fallback;
        }
    }

    public String hostname() {
        return hostname;
    }

    public String appVersion() {
        return appVersion;
    }

    public String gfeVersion() {
        return gfeVersion;
    }

    public int httpsPort() {
        return httpsPort;
    }

    public int externalPort() {
        return externalPort;
    }

    public long serverCodecModeSupport() {
        return serverCodecModeSupport;
    }

    public long maxLumaPixelsH264() {
        return maxLumaPixelsH264;
    }

    public long maxLumaPixelsHEVC() {
        return maxLumaPixelsHEVC;
    }

    public String gpuType() {
        return gpuType;
    }

    public boolean paired() {
        return paired;
    }

    public int runningGameId() {
        return runningGameId;
    }

    public String rawXml() {
        return rawXml;
    }

    public int majorVersion() {
        if (appVersion == null) {
            return 0;
        }
        String[] parts = appVersion.split("\\.");
        if (parts.length != 4) {
            return 0;
        }
        try {
            return Integer.parseInt(parts[0]);
        } catch (NumberFormatException e) {
            return 0;
        }
    }
}
