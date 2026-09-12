package expo.modules.zenremotedesktop.moonlight;

import org.bouncycastle.asn1.x500.X500Name;
import org.bouncycastle.asn1.x500.X500NameBuilder;
import org.bouncycastle.asn1.x500.style.BCStyle;
import org.bouncycastle.asn1.x509.SubjectPublicKeyInfo;
import org.bouncycastle.cert.X509v3CertificateBuilder;
import org.bouncycastle.cert.jcajce.JcaX509CertificateConverter;
import org.bouncycastle.jce.provider.BouncyCastleProvider;
import org.bouncycastle.openssl.jcajce.JcaPEMWriter;
import org.bouncycastle.operator.ContentSigner;
import org.bouncycastle.operator.jcajce.JcaContentSignerBuilder;
import org.bouncycastle.util.encoders.Base64;

import java.io.ByteArrayInputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.OutputStreamWriter;
import java.io.StringWriter;
import java.math.BigInteger;
import java.security.KeyFactory;
import java.security.KeyPair;
import java.security.KeyPairGenerator;
import java.security.NoSuchAlgorithmException;
import java.security.PrivateKey;
import java.security.Provider;
import java.security.SecureRandom;
import java.security.cert.CertificateException;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.security.spec.InvalidKeySpecException;
import java.security.spec.PKCS8EncodedKeySpec;
import java.util.Calendar;
import java.util.Date;
import java.util.Locale;

/**
 * Zen client identity provider, adapted from upstream AndroidCryptoProvider
 * (moonlight-android@98c12beb, app/src/main/java/com/limelight/binding/crypto/AndroidCryptoProvider.java).
 *
 * The only adaptation is the storage location: upstream uses the Android
 * app-private files directory, while Zen binds the identity to an explicit
 * Zen-owned private directory (0700, enforced by the caller). Certificate
 * generation, PEM encoding, PKCS#8 persistence and base64 handling are upstream.
 */
public class ZenCryptoProvider implements LimelightCryptoProvider {

    private final File certFile;
    private final File keyFile;

    private X509Certificate cert;
    private PrivateKey key;
    private byte[] pemCertBytes;

    private static final Object globalCryptoLock = new Object();

    private static final Provider bcProvider = new BouncyCastleProvider();

    public ZenCryptoProvider(File identityDir) {
        this.certFile = new File(identityDir, "client.crt");
        this.keyFile = new File(identityDir, "client.key");
    }

    private byte[] loadFileToBytes(File file) {
        if (!file.exists()) {
            return null;
        }
        try (FileInputStream in = new FileInputStream(file)) {
            byte[] fileData = new byte[(int) file.length()];
            if (in.read(fileData) != file.length()) {
                return null;
            }
            return fileData;
        } catch (IOException e) {
            return null;
        }
    }

    private boolean loadCertKeyPair() {
        byte[] certBytes = loadFileToBytes(certFile);
        byte[] keyBytes = loadFileToBytes(keyFile);
        if (certBytes == null || keyBytes == null) {
            MoonlightLog.info("Missing client cert or key; generating a new one");
            return false;
        }
        try {
            CertificateFactory certFactory = CertificateFactory.getInstance("X.509", bcProvider);
            cert = (X509Certificate) certFactory.generateCertificate(new ByteArrayInputStream(certBytes));
            pemCertBytes = certBytes;
            KeyFactory keyFactory = KeyFactory.getInstance("RSA", bcProvider);
            key = keyFactory.generatePrivate(new PKCS8EncodedKeySpec(keyBytes));
        } catch (CertificateException e) {
            MoonlightLog.warning("Corrupted client certificate");
            return false;
        } catch (NoSuchAlgorithmException e) {
            throw new RuntimeException(e);
        } catch (InvalidKeySpecException e) {
            MoonlightLog.warning("Corrupted client key");
            return false;
        }
        return true;
    }

    private boolean generateCertKeyPair() {
        byte[] snBytes = new byte[8];
        new SecureRandom().nextBytes(snBytes);

        KeyPair keyPair;
        try {
            KeyPairGenerator keyPairGenerator = KeyPairGenerator.getInstance("RSA", bcProvider);
            keyPairGenerator.initialize(2048);
            keyPair = keyPairGenerator.generateKeyPair();
        } catch (NoSuchAlgorithmException e) {
            throw new RuntimeException(e);
        }

        Date now = new Date();
        Calendar calendar = Calendar.getInstance();
        calendar.setTime(now);
        calendar.add(Calendar.YEAR, 20);
        Date expirationDate = calendar.getTime();
        BigInteger serial = new BigInteger(snBytes).abs();

        X500NameBuilder nameBuilder = new X500NameBuilder(BCStyle.INSTANCE);
        nameBuilder.addRDN(BCStyle.CN, "NVIDIA GameStream Client");
        X500Name name = nameBuilder.build();

        X509v3CertificateBuilder certBuilder = new X509v3CertificateBuilder(name, serial, now, expirationDate,
                Locale.ENGLISH, name, SubjectPublicKeyInfo.getInstance(keyPair.getPublic().getEncoded()));

        try {
            ContentSigner sigGen = new JcaContentSignerBuilder("SHA256withRSA")
                    .setProvider(bcProvider).build(keyPair.getPrivate());
            cert = new JcaX509CertificateConverter().setProvider(bcProvider)
                    .getCertificate(certBuilder.build(sigGen));
            key = keyPair.getPrivate();
        } catch (Exception e) {
            throw new RuntimeException(e);
        }

        MoonlightLog.info("Generated a new client key pair");
        saveCertKeyPair();
        return true;
    }

    private void saveCertKeyPair() {
        try (FileOutputStream certOut = new FileOutputStream(certFile);
             FileOutputStream keyOut = new FileOutputStream(keyFile)) {
            StringWriter strWriter = new StringWriter();
            try (JcaPEMWriter pemWriter = new JcaPEMWriter(strWriter)) {
                pemWriter.writeObject(cert);
            }
            // Line endings MUST be UNIX for the host to accept the cert properly.
            try (OutputStreamWriter certWriter = new OutputStreamWriter(certOut)) {
                String pemStr = strWriter.getBuffer().toString();
                for (int i = 0; i < pemStr.length(); i++) {
                    char c = pemStr.charAt(i);
                    if (c != '\r') {
                        certWriter.append(c);
                    }
                }
            }
            keyOut.write(key.getEncoded());
        } catch (IOException e) {
            // Not fatal here, but the next run has to generate a new identity.
            MoonlightLog.warning("Failed to persist client key pair");
        }
    }

    @Override
    public X509Certificate getClientCertificate() {
        synchronized (globalCryptoLock) {
            if (cert != null) {
                return cert;
            }
            if (loadCertKeyPair()) {
                return cert;
            }
            if (!generateCertKeyPair()) {
                return null;
            }
            loadCertKeyPair();
            return cert;
        }
    }

    @Override
    public PrivateKey getClientPrivateKey() {
        synchronized (globalCryptoLock) {
            if (key != null) {
                return key;
            }
            if (loadCertKeyPair()) {
                return key;
            }
            if (!generateCertKeyPair()) {
                return null;
            }
            loadCertKeyPair();
            return key;
        }
    }

    @Override
    public byte[] getPemEncodedClientCertificate() {
        synchronized (globalCryptoLock) {
            getClientCertificate();
            return pemCertBytes;
        }
    }

    @Override
    public String encodeBase64String(byte[] data) {
        return Base64.toBase64String(data);
    }
}
