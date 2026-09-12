package expo.modules.zenremotedesktop.moonlight

import org.junit.Assert.assertEquals
import org.junit.Test
import java.security.KeyFactory
import java.security.Signature
import java.security.spec.PKCS8EncodedKeySpec
import java.util.Base64

/**
 * Cross-language contract: the Android signer must produce the exact signature
 * the Go verifier (sunshine_enroll_vector_test.go) expects for the shared
 * vector. This pins the domain-separated enrollment message and RSA SHA-256
 * PKCS#1 v1.5 encoding on both sides.
 */
class MoonlightEnrollmentSignerTest {

  @Test
  fun nativeSignerMatchesGoVerifierVector() {
    val pkcs8 = Base64.getDecoder().decode(PKCS8_B64)
    val key = KeyFactory.getInstance("RSA").generatePrivate(PKCS8EncodedKeySpec(pkcs8))
    val message = "zen-moonlight-enroll-v1\u0000$ATTEMPT\n$NONCE".toByteArray(Charsets.UTF_8)
    val signature = Signature.getInstance("SHA256withRSA").run {
      initSign(key)
      update(message)
      sign()
    }
    assertEquals(EXPECTED_SIGNATURE_HEX, signature.joinToString("") { "%02x".format(it) })
  }

  private companion object {
    const val PKCS8_B64 = "MIIEvAIBADANBgkqhkiG9w0BAQEFAASCBKYwggSiAgEAAoIBAQDQ5om1q6vZsTulxtmy8ZxovPdFQheGve++0mLcsKG5Dcc3WniHZFa7EkdOqLr8LtXonE1c9oTiIL+kfb8cd0jOyqIFteMA+zRFBLGUuE4/SvryS7A8NHJEiChpQQAxYE5x6qYMxwEa9ADlih/9nUo0Sdzk4gcvQPzkHmStIFCNyq4KGIVD9+cq2TMUbYEglMmMOcmUV29u/aeqw/R6yWLDjRspqUzXLaEsYlBlzrnSpoaUahF819g0qmeubVcUlh/TAaWPh6AlTGYFmm3UVwMM2IiOxk/W82QdJYqdJof1j2zbd6riCfWRtLCnFTqMzdfJ4F9YFELwQAYDYziH/LF1AgMBAAECggEAA1uGixefQ3hzGk7/4L28DdLmW9lGwjD7UXiALkaIfY5DIsNJfKOYNpmB8Tm1B6iZDOFg55rut9O0QNM55x3jsFnWu3tuPUMhq++ktbHgpWkcb23X/0pjK8bPH3dJUYHYFkH3OUKu5IgCG65Z+pw0knKSzPr1B/AQyyR8lGvDNQQtfP1rgBY7fKx6PQrNpAGcY4hhCujVCbHJKBWUVUakTvr0Vq2xVeaT1bRFGSmxyuX29QB4BORYLDKBInzTnt+eErlNvNtpUGXU6Ov9gfLqjF3QG/EsvEcKf4s0N1pTccOgxhQ8oBadJ3TODbA613sVXdxYAxx5jKjG3vU+qhwqYQKBgQDfEeJFP1PaGNjYnpJODLEiOUObMbURMFILLDNz9GhKj+rhUbjTVxKkv3/UU5if+gEWJQADE3z3+uQDvOyzi1pjCmORd8Rur7LeN/ke4Lb6X+ZDZwGFnGsw9EEyjADxeDimg69R6O+m1zjUquI9I3mogHH7IgP6y+R6MGhi6fdcTwKBgQDvvS0OuGRREBDR/WFaQMpIGWsGDUb/p/LbjccqSBYthNNSuPwjE+TFl6JKJKbM5vamJ51VDEOcCxvwqg8IY4EtoeIEVllZXS3zsOUXtz+rhfj1qROxkmdxuK5JL/Y+pmUQdax4U7KiJ4ltM7R7NqITMGM+GwyOgmo+Hc6xevvQ+wKBgHOR7mr2Dll2ehJwxVgOl08l3/Lt4+ON51PGiLnQrJ/ExGoMTvefqxcT6AR3cyGfAyUX8lOlqx9HKw8MuI2k6yVY4pEhPfIisUcUNMtcnTBGsyPEoDM7AQYR5h1sD6kLIj6TBygmyNLlupnkFuaaFJPKSENWMj2jmTH9Fnf4w6FdAoGAPznhHDS9IDPge8EbX7Yeow0xEJOH2fztK8IkeJ4yWybgpLxsosOoXgQzpOItq3RuMDvaXoexfQHhCIORG2FCvEopVYOAZPUSHWbVxH9rp0zZ78/7haVa6r8OF/cyNiukE8c1CTbpsaJDuC0euDAcZnsocUSo9xyl8GPkEyKgLukCgYBAFEPPW/7oIUIfzjn2HaodMceP0O5b9Zj3Wq8LGwivyxZDgEYf+CGlDWMGaJC3V62FWgHGesDW8nkLe4MWyUonT1jg96WG73dFPQ0VffObuz/rqZ1DzCyskohmwdLKpdNHWvkJhTHFsqKHomCTUrbc0gCJHQO7XiEMCyC4ID39bA=="
    const val ATTEMPT = "00ff00ff00ff00ff00ff00ff00ff00ff"
    const val NONCE = "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff"
    const val EXPECTED_SIGNATURE_HEX = "8dd1a60c74bc099327bd1e9c48054df7fd168e681d6dffc8430adaa72179cd13485a905cfa7eff50dbca2c05f19664adf04fe08cf9e41c8ff234a4b60b7c74bb296bf391607de67ff265d5e3a26bcbf928797f22b95bdb6b00bf5d9249d5db972320618bb17ff737a61b3e27b9b176aaf9b620c041096b5c768e09bb1c0ab4d5c958ce7071020431ee3fc15929900194e1bd0c269ad5e5bf25be04bfd5c34bf9380d8debd374e0e1908f98530a7e5d170182b8744e67108756f9b53df31251a162e09a29c1ed5fff457e22e52b968cf85e9bea6715dd5172007003c663e7da11775ae8f63a7ba0114a9cc2e730834149ab1a3cef3ddefc91fdc3732cc5e634ca"
  }
}
