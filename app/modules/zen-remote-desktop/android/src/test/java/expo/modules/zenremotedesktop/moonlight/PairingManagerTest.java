package expo.modules.zenremotedesktop.moonlight;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import org.bouncycastle.crypto.BlockCipher;
import org.bouncycastle.crypto.engines.AESLightEngine;
import org.bouncycastle.crypto.params.KeyParameter;
import org.junit.Test;

import java.io.ByteArrayInputStream;
import java.io.File;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.security.KeyPair;
import java.security.KeyPairGenerator;
import java.security.MessageDigest;
import java.security.PrivateKey;
import java.security.PublicKey;
import java.security.SecureRandom;
import java.security.Signature;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.util.Arrays;
import java.util.HashMap;
import java.util.Map;

/**
 * Executable in-process pairing regression: the vendored upstream PairingManager
 * runs the real challenge/response key exchange against an inert server that
 * mirrors Sunshine's pairing steps with its own generated identity. No network,
 * host or APK is involved; the crypto under test is the upstream code.
 */
public class PairingManagerTest {

    private static final String SERVER_INFO =
            "<root status_code=\"200\"><appversion>7.1.431.0</appversion></root>";

    @Test
    public void pairsWithCorrectPinUsingUpstreamExchange() throws Exception {
        FakeSunshineServer server = new FakeSunshineServer("1234", false, false);
        ZenCryptoProvider client = clientProvider();
        PairingManager manager = new PairingManager(server, client);

        assertEquals(PairingManager.PairState.PAIRED, manager.pair(SERVER_INFO, "1234"));
        assertNotNull(manager.getPairedCert());
        assertEquals(server.serverCert, manager.getPairedCert());
        assertEquals(server.serverCert, server.pinnedCert);
        assertEquals(0, server.unpairCalls);
    }

    @Test
    public void wrongPinReturnsPinWrong() throws Exception {
        FakeSunshineServer server = new FakeSunshineServer("1234", false, false);
        PairingManager manager = new PairingManager(server, clientProvider());

        assertEquals(PairingManager.PairState.PIN_WRONG, manager.pair(SERVER_INFO, "0000"));
        assertEquals(1, server.unpairCalls);
    }

    @Test
    public void mitmSignatureIsRejected() throws Exception {
        FakeSunshineServer server = new FakeSunshineServer("1234", true, false);
        PairingManager manager = new PairingManager(server, clientProvider());

        assertEquals(PairingManager.PairState.FAILED, manager.pair(SERVER_INFO, "1234"));
    }

    @Test
    public void missingPlainCertMeansAnotherClientIsPairing() throws Exception {
        FakeSunshineServer server = new FakeSunshineServer("1234", false, true);
        PairingManager manager = new PairingManager(server, clientProvider());

        assertEquals(PairingManager.PairState.ALREADY_IN_PROGRESS, manager.pair(SERVER_INFO, "1234"));
        assertEquals(1, server.unpairCalls);
    }

    @Test
    public void pinStringIsFourDigits() {
        String pin = PairingManager.generatePinString();
        assertEquals(4, pin.length());
        assertTrue(pin.matches("[0-9]{4}"));
    }

    private static ZenCryptoProvider clientProvider() throws Exception {
        File dir = Files.createTempDirectory("zen-pairing-client").toFile();
        return new ZenCryptoProvider(dir);
    }

    /** Inert Sunshine-side pairing steps using the upstream wire format. */
    private static final class FakeSunshineServer implements MoonlightPairingHttp {
        final X509Certificate serverCert;
        X509Certificate pinnedCert;
        final PrivateKey serverKey;
        final boolean mitm;
        final boolean omitPlainCert;
        final String pin;
        final byte[] serverSecret = randomBytes(16);
        final PrivateKey otherKey;

        X509Certificate clientCert;
        byte[] clientCertSignature;
        byte[] aesKey;
        byte[] serverChallenge;
        byte[] storedChallengeResponse;
        int unpairCalls;

        FakeSunshineServer(String pin, boolean mitm, boolean omitPlainCert) throws Exception {
            File dir = Files.createTempDirectory("zen-pairing-server").toFile();
            ZenCryptoProvider serverIdentity = new ZenCryptoProvider(dir);
            this.serverCert = serverIdentity.getClientCertificate();
            this.serverKey = serverIdentity.getClientPrivateKey();
            this.pin = pin;
            this.mitm = mitm;
            this.omitPlainCert = omitPlainCert;
            KeyPairGenerator generator = KeyPairGenerator.getInstance("RSA");
            generator.initialize(2048);
            KeyPair pair = generator.generateKeyPair();
            this.otherKey = pair.getPrivate();
            this.pinnedCert = null;
        }

        @Override
        public int getServerMajorVersion(String serverInfo) {
            return 7;
        }

        @Override
        public String executePairingCommand(String additionalArguments, boolean enableReadTimeout)
                throws HostHttpResponseException, IOException {
            try {
                return executePairingCommandImpl(additionalArguments, enableReadTimeout);
            } catch (HostHttpResponseException e) {
                throw e;
            } catch (Exception e) {
                throw new IOException(e);
            }
        }

        private String executePairingCommandImpl(String additionalArguments, boolean enableReadTimeout) throws Exception {
            Map<String, String> params = parse(additionalArguments);
            if ("getservercert".equals(params.get("phrase"))) {
                byte[] salt = hexToBytes(params.get("salt"));
                clientCert = parsePem(hexToBytes(params.get("clientcert")));
                clientCertSignature = clientCert.getSignature();
                aesKey = Arrays.copyOf(sha256(concat(salt, pin.getBytes(StandardCharsets.UTF_8))), 16);
                StringBuilder response = new StringBuilder(ok("paired", "1"));
                if (!omitPlainCert) {
                    response.append(tag("plaincert", hex(serverCert.getEncoded())));
                }
                return wrap(response.toString());
            }
            if (params.containsKey("clientchallenge")) {
                byte[] clientChallenge = decryptAes(hexToBytes(params.get("clientchallenge")), aesKey);
                byte[] serverResponse = sha256(concat(concat(clientChallenge, serverCert.getSignature()), serverSecret));
                serverChallenge = randomBytes(16);
                byte[] encrypted = encryptAes(concat(serverResponse, serverChallenge), aesKey);
                return wrap(ok("paired", "1") + tag("challengeresponse", hex(encrypted)));
            }
            if (params.containsKey("serverchallengeresp")) {
                storedChallengeResponse = decryptAes(hexToBytes(params.get("serverchallengeresp")), aesKey);
                byte[] signature = sign(serverSecret, mitm ? otherKey : serverKey);
                return wrap(ok("paired", "1") + tag("pairingsecret", hex(concat(serverSecret, signature))));
            }
            if (params.containsKey("clientpairingsecret")) {
                byte[] data = hexToBytes(params.get("clientpairingsecret"));
                byte[] clientSecret = Arrays.copyOfRange(data, 0, 16);
                byte[] signature = Arrays.copyOfRange(data, 16, data.length);
                boolean signatureOk = verify(clientSecret, signature, clientCert.getPublicKey());
                byte[] expected = sha256(concat(concat(serverChallenge, clientCertSignature), clientSecret));
                boolean challengeOk = storedChallengeResponse != null
                        && Arrays.equals(expected, Arrays.copyOf(storedChallengeResponse, expected.length));
                return wrap(ok("paired", signatureOk && challengeOk ? "1" : "0"));
            }
            throw new IllegalStateException("unexpected pairing command: " + params.keySet());
        }

        @Override
        public String executePairingChallenge() {
            return wrap(ok("paired", "1"));
        }

        @Override
        public void unpair() {
            unpairCalls++;
        }

        @Override
        public void setServerCert(X509Certificate serverCert) {
            this.pinnedCert = serverCert;
        }
    }

    private static String wrap(String body) {
        return "<?xml version=\"1.0\"?><root status_code=\"200\">" + body + "</root>";
    }

    private static String ok(String tag, String value) {
        return tag(tag, value);
    }

    private static String tag(String tag, String value) {
        return "<" + tag + ">" + value + "</" + tag + ">";
    }

    private static Map<String, String> parse(String arguments) {
        Map<String, String> params = new HashMap<>();
        for (String part : arguments.split("&")) {
            int equals = part.indexOf('=');
            if (equals > 0) {
                params.put(part.substring(0, equals), part.substring(equals + 1));
            }
        }
        return params;
    }

    private static X509Certificate parsePem(byte[] pem) throws Exception {
        return (X509Certificate) CertificateFactory.getInstance("X.509")
                .generateCertificate(new ByteArrayInputStream(pem));
    }

    private static byte[] encryptAes(byte[] input, byte[] key) {
        BlockCipher cipher = new AESLightEngine();
        cipher.init(true, new KeyParameter(key));
        return blockCipher(cipher, input);
    }

    private static byte[] decryptAes(byte[] input, byte[] key) {
        BlockCipher cipher = new AESLightEngine();
        cipher.init(false, new KeyParameter(key));
        return blockCipher(cipher, input);
    }

    private static byte[] blockCipher(BlockCipher cipher, byte[] input) {
        int blockSize = cipher.getBlockSize();
        int rounded = (input.length + (blockSize - 1)) & ~(blockSize - 1);
        byte[] in = Arrays.copyOf(input, rounded);
        byte[] out = new byte[rounded];
        for (int offset = 0; offset < rounded; offset += blockSize) {
            cipher.processBlock(in, offset, out, offset);
        }
        return out;
    }

    private static byte[] sign(byte[] data, PrivateKey key) throws Exception {
        Signature signature = Signature.getInstance("SHA256withRSA");
        signature.initSign(key);
        signature.update(data);
        return signature.sign();
    }

    private static boolean verify(byte[] data, byte[] signature, PublicKey key) throws Exception {
        Signature verifier = Signature.getInstance("SHA256withRSA");
        verifier.initVerify(key);
        verifier.update(data);
        return verifier.verify(signature);
    }

    private static byte[] sha256(byte[] data) throws Exception {
        return MessageDigest.getInstance("SHA-256").digest(data);
    }

    private static byte[] randomBytes(int length) {
        byte[] bytes = new byte[length];
        new SecureRandom().nextBytes(bytes);
        return bytes;
    }

    private static byte[] concat(byte[] a, byte[] b) {
        byte[] result = new byte[a.length + b.length];
        System.arraycopy(a, 0, result, 0, a.length);
        System.arraycopy(b, 0, result, a.length, b.length);
        return result;
    }

    private static final char[] HEX = "0123456789ABCDEF".toCharArray();

    private static String hex(byte[] bytes) {
        char[] out = new char[bytes.length * 2];
        for (int i = 0; i < bytes.length; i++) {
            int value = bytes[i] & 0xFF;
            out[i * 2] = HEX[value >>> 4];
            out[i * 2 + 1] = HEX[value & 0x0F];
        }
        return new String(out);
    }

    private static byte[] hexToBytes(String text) {
        byte[] data = new byte[text.length() / 2];
        for (int i = 0; i < data.length; i++) {
            data[i] = (byte) ((Character.digit(text.charAt(i * 2), 16) << 4)
                    + Character.digit(text.charAt(i * 2 + 1), 16));
        }
        return data;
    }
}
