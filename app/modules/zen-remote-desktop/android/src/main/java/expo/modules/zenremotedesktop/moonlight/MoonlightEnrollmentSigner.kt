package expo.modules.zenremotedesktop.moonlight

import java.io.File
import java.security.Signature
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.security.spec.PKCS8EncodedKeySpec
import java.security.KeyFactory
import java.security.MessageDigest

/**
 * Production enrollment signer used by the Expo module. The Moonlight client
 * private key stays inside this native identity; callers receive only the
 * certificate PEM, its SHA-256 fingerprint and detached signatures.
 */
object MoonlightEnrollmentSigner {

  const val DOMAIN = "zen-moonlight-enroll-v1\u0000"

  fun message(attempt: String, nonce: String): ByteArray =
    (DOMAIN + attempt + "\n" + nonce).toByteArray(Charsets.UTF_8)

  fun certificate(identityDir: File): X509Certificate {
    val crypto = ZenCryptoProvider(identityDir)
    val certificate = crypto.clientCertificate
      ?: throw IllegalStateException("moonlight_identity_unavailable")
    // Touch the private key here so a corrupt/missing key fails before pairing.
    if (crypto.clientPrivateKey == null) {
      throw IllegalStateException("moonlight_identity_unavailable")
    }
    return certificate
  }

  fun certificatePem(identityDir: File): String {
    certificate(identityDir)
    val pem = ZenCryptoProvider(identityDir).pemEncodedClientCertificate
      ?: throw IllegalStateException("moonlight_identity_unavailable")
    return String(pem, Charsets.US_ASCII)
  }

  fun fingerprint(identityDir: File): String {
    val digest = MessageDigest.getInstance("SHA-256").digest(certificate(identityDir).encoded)
    return digest.joinToString("") { "%02x".format(it) }
  }

  fun sign(identityDir: File, attempt: String, nonce: String): String {
    val crypto = ZenCryptoProvider(identityDir)
    val key = crypto.clientPrivateKey ?: throw IllegalStateException("moonlight_identity_unavailable")
    val algorithm = when (key.algorithm) {
      "RSA" -> "SHA256withRSA"
      "EC" -> "SHA256withECDSA"
      else -> throw IllegalStateException("moonlight_identity_unsupported_key")
    }
    val signature = Signature.getInstance(algorithm).run {
      initSign(key)
      update(message(attempt, nonce))
      sign()
    }
    return signature.joinToString("") { "%02x".format(it) }
  }

  /** PKCS#8 export exists only for tests; production code never calls it. */
  internal fun privateKeyPkcs8ForTest(pkcs8: ByteArray) =
    KeyFactory.getInstance("RSA").generatePrivate(PKCS8EncodedKeySpec(pkcs8))
}
