package expo.modules.zenremotedesktop.moonlight;

import org.bouncycastle.openssl.jcajce.JcaPEMWriter;

import java.io.ByteArrayInputStream;
import java.io.File;
import java.io.IOException;
import java.io.StringWriter;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.security.cert.CertificateException;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;

/**
 * Zen-owned pinned host certificate store. The certificate comes from upstream
 * pairing (PairingManager.getPairedCert) and is reused for TLS pinning on later
 * connections so Zen never falls back to trusting an unpinned cert silently.
 */
public final class ZenHostTrustStore {

    private final File certFile;

    public ZenHostTrustStore(File identityDir) {
        this.certFile = new File(identityDir, "server.crt");
    }

    public File file() {
        return certFile;
    }

    /** Returns the pinned certificate or null when this host is not paired yet. */
    public X509Certificate load() throws IOException, CertificateException {
        if (!certFile.isFile() || certFile.length() == 0) {
            return null;
        }
        byte[] pem = Files.readAllBytes(certFile.toPath());
        return (X509Certificate) CertificateFactory.getInstance("X.509")
                .generateCertificate(new ByteArrayInputStream(pem));
    }

    public void save(X509Certificate certificate) throws IOException {
        StringWriter writer = new StringWriter();
        try (JcaPEMWriter pemWriter = new JcaPEMWriter(writer)) {
            pemWriter.writeObject(certificate);
        }
        String pem = writer.toString().replace("\r", "");
        // Atomic replace: a crashed write never leaves a half certificate that
        // the next connect would reject as corrupt.
        java.nio.file.Path temp = java.nio.file.Paths.get(certFile.getPath() + ".tmp");
        Files.write(temp, pem.getBytes(StandardCharsets.US_ASCII));
        try {
            Files.move(temp, certFile.toPath(),
                    java.nio.file.StandardCopyOption.REPLACE_EXISTING,
                    java.nio.file.StandardCopyOption.ATOMIC_MOVE);
        } catch (java.nio.file.AtomicMoveNotSupportedException e) {
            Files.move(temp, certFile.toPath(), java.nio.file.StandardCopyOption.REPLACE_EXISTING);
        }
    }

    public void clear() throws IOException {
        Files.deleteIfExists(certFile.toPath());
    }
}
