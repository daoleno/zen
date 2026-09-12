package expo.modules.zenremotedesktop.moonlight;

import java.io.FileNotFoundException;
import java.io.IOException;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.Proxy;
import java.net.Socket;
import java.security.KeyManagementException;
import java.security.KeyStore;
import java.security.KeyStoreException;
import java.security.NoSuchAlgorithmException;
import java.security.Principal;
import java.security.PrivateKey;
import java.security.SecureRandom;
import java.security.cert.Certificate;
import java.security.cert.CertificateException;
import java.security.cert.X509Certificate;
import java.util.UUID;
import java.util.concurrent.TimeUnit;

import javax.net.ssl.HostnameVerifier;
import javax.net.ssl.HttpsURLConnection;
import javax.net.ssl.KeyManager;
import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLHandshakeException;
import javax.net.ssl.SSLPeerUnverifiedException;
import javax.net.ssl.SSLSession;
import javax.net.ssl.TrustManager;
import javax.net.ssl.TrustManagerFactory;
import javax.net.ssl.X509KeyManager;
import javax.net.ssl.X509TrustManager;

import okhttp3.ConnectionPool;
import okhttp3.HttpUrl;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.Response;
import okhttp3.ResponseBody;

/**
 * Host HTTP/HTTPS client ported from upstream NvHTTP
 * (moonlight-android@98c12bebffac592eb57cf25e9a4638b40aa2c17d,
 * app/src/main/java/com/limelight/nvstream/http/NvHTTP.java).
 *
 * Kept upstream semantics: TLS chain validation first, pinned self-signed
 * certificate equality second, hostname verification bypassed only for the
 * exact pinned certificate, pairing commands over the unpinned HTTP endpoint,
 * the pairing challenge over HTTPS, and /serverinfo falling back to HTTP when
 * the pinned certificate mismatches. Query strings (which carry the RI key)
 * are never logged.
 */
public class MoonlightNvHttp implements MoonlightPairingHttp, MoonlightHost {

    public static final int DEFAULT_HTTP_PORT = 47989;
    public static final int DEFAULT_HTTPS_PORT = 47984;
    public static final int SHORT_CONNECTION_TIMEOUT_MS = 3000;
    public static final int LONG_CONNECTION_TIMEOUT_MS = 5000;
    public static final int READ_TIMEOUT_MS = 7000;

    private final String address;
    private final String uniqueId;
    private final String deviceName;

    private final HttpUrl baseUrlHttp;
    private final int httpsPort;

    private OkHttpClient httpClientLongConnectTimeout;
    private OkHttpClient httpClientShortConnectTimeout;
    private OkHttpClient httpClientLongConnectNoReadTimeout;

    private X509TrustManager defaultTrustManager;
    private X509TrustManager trustManager;
    private X509KeyManager keyManager;
    private volatile X509Certificate serverCert;

    private final PairingManager pairingManager;

    public MoonlightNvHttp(String host, int httpPort, int httpsPort, String uniqueId, String deviceName,
                           X509Certificate serverCert, LimelightCryptoProvider cryptoProvider)
            throws IOException {
        this.address = host;
        this.uniqueId = uniqueId;
        this.deviceName = deviceName;
        this.httpsPort = httpsPort;
        this.serverCert = serverCert;

        initializeHttpState(cryptoProvider);

        try {
            // OkHttp chokes on IPv4-mapped IPv6 textual addresses.
            String addressString = host;
            if (addressString.contains(":") && addressString.contains(".")) {
                InetAddress addr = InetAddress.getByName(addressString);
                if (addr instanceof Inet4Address) {
                    addressString = ((Inet4Address) addr).getHostAddress();
                }
            }
            this.baseUrlHttp = new HttpUrl.Builder()
                    .scheme("http")
                    .host(addressString)
                    .port(httpPort)
                    .build();
        } catch (IllegalArgumentException e) {
            throw new IOException(e);
        }

        this.pairingManager = new PairingManager(this, cryptoProvider);
    }

    private static X509TrustManager getDefaultTrustManager() {
        try {
            TrustManagerFactory tmf = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
            tmf.init((KeyStore) null);
            for (TrustManager tm : tmf.getTrustManagers()) {
                if (tm instanceof X509TrustManager) {
                    return (X509TrustManager) tm;
                }
            }
        } catch (NoSuchAlgorithmException | KeyStoreException e) {
            throw new RuntimeException(e);
        }
        throw new IllegalStateException("No X509 trust manager found");
    }

    private void initializeHttpState(final LimelightCryptoProvider cryptoProvider) {
        keyManager = new X509KeyManager() {
            public String chooseClientAlias(String[] keyTypes, Principal[] issuers, Socket socket) {
                return "Limelight-RSA";
            }

            public String chooseServerAlias(String keyType, Principal[] issuers, Socket socket) {
                return null;
            }

            public X509Certificate[] getCertificateChain(String alias) {
                return new X509Certificate[]{cryptoProvider.getClientCertificate()};
            }

            public String[] getClientAliases(String keyType, Principal[] issuers) {
                return null;
            }

            public PrivateKey getPrivateKey(String alias) {
                return cryptoProvider.getClientPrivateKey();
            }

            public String[] getServerAliases(String keyType, Principal[] issuers) {
                return null;
            }
        };

        defaultTrustManager = getDefaultTrustManager();
        trustManager = new X509TrustManager() {
            public X509Certificate[] getAcceptedIssuers() {
                return new X509Certificate[0];
            }

            public void checkClientTrusted(X509Certificate[] certs, String authType) {
                throw new IllegalStateException("Should never be called");
            }

            public void checkServerTrusted(X509Certificate[] certs, String authType)
                    throws CertificateException {
                try {
                    defaultTrustManager.checkServerTrusted(certs, authType);
                } catch (CertificateException e) {
                    // Accept only the exact pinned self-signed certificate.
                    if (certs.length == 1 && MoonlightNvHttp.this.serverCert != null) {
                        if (!certs[0].equals(MoonlightNvHttp.this.serverCert)) {
                            throw new CertificateException("Certificate mismatch");
                        }
                    } else {
                        throw e;
                    }
                }
            }
        };

        HostnameVerifier hostnameVerifier = new HostnameVerifier() {
            public boolean verify(String hostname, SSLSession session) {
                try {
                    Certificate[] certificates = session.getPeerCertificates();
                    if (certificates.length == 1 && certificates[0].equals(MoonlightNvHttp.this.serverCert)) {
                        return true;
                    }
                } catch (SSLPeerUnverifiedException e) {
                    // Fall through to the default verifier.
                }
                return HttpsURLConnection.getDefaultHostnameVerifier().verify(hostname, session);
            }
        };

        httpClientLongConnectTimeout = new OkHttpClient.Builder()
                .connectionPool(new ConnectionPool(0, 1, TimeUnit.MILLISECONDS))
                .hostnameVerifier(hostnameVerifier)
                .readTimeout(READ_TIMEOUT_MS, TimeUnit.MILLISECONDS)
                .connectTimeout(LONG_CONNECTION_TIMEOUT_MS, TimeUnit.MILLISECONDS)
                .proxy(Proxy.NO_PROXY)
                .build();
        httpClientShortConnectTimeout = httpClientLongConnectTimeout.newBuilder()
                .connectTimeout(SHORT_CONNECTION_TIMEOUT_MS, TimeUnit.MILLISECONDS)
                .build();
        httpClientLongConnectNoReadTimeout = httpClientLongConnectTimeout.newBuilder()
                .readTimeout(0, TimeUnit.MILLISECONDS)
                .build();
    }

    private OkHttpClient performTlsSetup(OkHttpClient client) {
        try {
            SSLContext sc = SSLContext.getInstance("TLS");
            sc.init(new KeyManager[]{keyManager}, new TrustManager[]{trustManager}, new SecureRandom());
            return client.newBuilder().sslSocketFactory(sc.getSocketFactory(), trustManager).build();
        } catch (NoSuchAlgorithmException | KeyManagementException e) {
            throw new RuntimeException(e);
        }
    }

    private HttpUrl getHttpsUrl() {
        return new HttpUrl.Builder().scheme("https").host(baseUrlHttp.host()).port(httpsPort).build();
    }

    private HttpUrl getCompleteUrl(HttpUrl baseUrl, String path, String query) {
        return baseUrl.newBuilder()
                .addPathSegment(path)
                .query(query)
                .addQueryParameter("uniqueid", uniqueId)
                .addQueryParameter("uuid", UUID.randomUUID().toString())
                .build();
    }

    private ResponseBody openHttpConnection(OkHttpClient client, HttpUrl baseUrl, String path, String query)
            throws IOException {
        HttpUrl completeUrl = getCompleteUrl(baseUrl, path, query);
        Request request = new Request.Builder().url(completeUrl).get().build();
        Response response = performTlsSetup(client).newCall(request).execute();
        ResponseBody body = response.body();
        if (response.isSuccessful()) {
            return body;
        }
        if (body != null) {
            body.close();
        }
        if (response.code() == 404) {
            throw new FileNotFoundException(completeUrl.toString());
        }
        throw new HostHttpResponseException(response.code(), response.message());
    }

    private String openHttpConnectionToString(OkHttpClient client, HttpUrl baseUrl, String path, String query)
            throws IOException {
        try (ResponseBody body = openHttpConnection(client, baseUrl, path, query)) {
            return body.string();
        }
    }

    public String getServerInfo() throws IOException, MoonlightParseException {
        if (serverCert != null) {
            try {
                try {
                    String resp = openHttpConnectionToString(httpClientLongConnectTimeout, getHttpsUrl(),
                            "serverinfo", null);
                    getServerVersion(resp);
                    return resp;
                } catch (SSLHandshakeException e) {
                    if (e.getCause() instanceof CertificateException) {
                        throw new HostHttpResponseException(401, "Server certificate mismatch");
                    }
                    throw e;
                }
            } catch (HostHttpResponseException e) {
                if (e.getErrorCode() == 401) {
                    return openHttpConnectionToString(httpClientLongConnectTimeout, baseUrlHttp, "serverinfo", null);
                }
                throw e;
            }
        }
        return openHttpConnectionToString(httpClientLongConnectTimeout, baseUrlHttp, "serverinfo", null);
    }

    @Override
    public String address() {
        return address;
    }

    @Override
    public MoonlightServerInfo fetchServerInfo() throws IOException, MoonlightParseException {
        return MoonlightServerInfo.parse(getServerInfo());
    }

    public String getServerVersion(String serverInfo) throws MoonlightParseException, IOException {
        // appversion is present in all supported server versions
        return MoonlightXml.getXmlString(serverInfo, "appversion", true);
    }

    @Override
    public int getServerMajorVersion(String serverInfo) throws MoonlightParseException, IOException {
        String version = getServerVersion(serverInfo);
        String[] parts = version.split("\\.");
        if (parts.length != 4) {
            throw new IllegalArgumentException("Malformed server version field: " + version);
        }
        return Integer.parseInt(parts[0]);
    }

    public PairingManager.PairState getPairState(String serverInfo) throws MoonlightParseException, IOException {
        return "1".equals(MoonlightXml.getXmlString(serverInfo, "PairStatus", true))
                ? PairingManager.PairState.PAIRED : PairingManager.PairState.NOT_PAIRED;
    }

    @Override
    public void setServerCert(X509Certificate serverCert) {
        this.serverCert = serverCert;
    }

    @Override
    public X509Certificate getServerCert() {
        return serverCert;
    }

    @Override
    public String executePairingCommand(String additionalArguments, boolean enableReadTimeout)
            throws HostHttpResponseException, IOException {
        return openHttpConnectionToString(enableReadTimeout ? httpClientLongConnectTimeout
                        : httpClientLongConnectNoReadTimeout,
                baseUrlHttp, "pair", "devicename=" + deviceName + "&updateState=1&" + additionalArguments);
    }

    @Override
    public String executePairingChallenge() throws HostHttpResponseException, IOException {
        return openHttpConnectionToString(httpClientLongConnectTimeout, getHttpsUrl(),
                "pair", "devicename=" + deviceName + "&updateState=1&phrase=pairchallenge");
    }

    @Override
    public void cancelInFlight() {
        httpClientLongConnectTimeout.dispatcher().cancelAll();
        httpClientShortConnectTimeout.dispatcher().cancelAll();
        httpClientLongConnectNoReadTimeout.dispatcher().cancelAll();
    }

    @Override
    public void unpair() throws IOException {
        openHttpConnectionToString(httpClientLongConnectTimeout, baseUrlHttp, "unpair", null);
    }

    /** Package-private test seam for the pinned-certificate trust manager. */
    X509TrustManager trustManagerForTesting() {
        return trustManager;
    }

    @Override
    public PairingManager getPairingManager() {
        return pairingManager;
    }

    /**
     * Sends /launch or /resume. The result reports host acceptance; the stream
     * becomes established only when the C core reports connectionStarted.
     */
    @Override
    public LaunchOutcome launchOrResume(MoonlightLaunchRequest request) throws IOException, MoonlightParseException {
        String xml = openHttpConnectionToString(httpClientLongConnectNoReadTimeout, getHttpsUrl(),
                request.verb(), request.toQuery());
        String resultTag = MoonlightLaunchRequest.VERB_RESUME.equals(request.verb()) ? "resume" : "gamesession";
        boolean accepted = !"0".equals(MoonlightXml.getXmlString(xml, resultTag, true));
        String sessionUrl = MoonlightXml.getXmlString(xml, "sessionUrl0", false);
        return new LaunchOutcome(accepted, sessionUrl);
    }

    @Override
    public boolean quitApp() throws IOException, MoonlightParseException {
        String xml = openHttpConnectionToString(httpClientLongConnectNoReadTimeout, getHttpsUrl(), "cancel", null);
        return !"0".equals(MoonlightXml.getXmlString(xml, "cancel", true));
    }

}
