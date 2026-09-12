package host

import (
	"encoding/base64"
	"encoding/pem"
	"testing"
)

// Cross-language enrollment proof vector. The exact bytes/signature below are
// also asserted by the Android JVM signer test, so a native signature must
// verify through the production Go verifier.
const (
	vectorCertDERB64 = "MIICvzCCAaegAwIBAgIBBzANBgkqhkiG9w0BAQsFADAjMSEwHwYDVQQDExhOVklESUEgR2FtZVN0cmVhbSBDbGllbnQwHhcNMjYwOTEyMjAxMzQwWhcNMjYwOTEyMjIxMzQwWjAjMSEwHwYDVQQDExhOVklESUEgR2FtZVN0cmVhbSBDbGllbnQwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDQ5om1q6vZsTulxtmy8ZxovPdFQheGve++0mLcsKG5Dcc3WniHZFa7EkdOqLr8LtXonE1c9oTiIL+kfb8cd0jOyqIFteMA+zRFBLGUuE4/SvryS7A8NHJEiChpQQAxYE5x6qYMxwEa9ADlih/9nUo0Sdzk4gcvQPzkHmStIFCNyq4KGIVD9+cq2TMUbYEglMmMOcmUV29u/aeqw/R6yWLDjRspqUzXLaEsYlBlzrnSpoaUahF819g0qmeubVcUlh/TAaWPh6AlTGYFmm3UVwMM2IiOxk/W82QdJYqdJof1j2zbd6riCfWRtLCnFTqMzdfJ4F9YFELwQAYDYziH/LF1AgMBAAEwDQYJKoZIhvcNAQELBQADggEBAJUhjtR6CMM5RzkwMwlgTWlTWBlnEjTvx672WchZKElV03XYdHEsDQQoLjM8arEYFF9HkyzN+cPIAiXsddOyL8QXN+mejpVPZ26xKeoP1g4GsJHBXfETqOP3YTM/s1Z6OGeZU9FJa88yA0CjY6Da6KWMdJf2D3wN85t1W/sIDoT6Xy743wsq6poEB4b1c+KrFCshGWl1pfxri47P6A7EBzFNdxn6RMEPXDduxHQjBK1+mkZ7ULnVcYoRT9aeeVX39IZH+uDkDygkJ0BIunco7/w53qN+OVUBcJ/MWsLxG9+3A7vDF+LXkdaxBN5v/QYV4/WVLOVopVbWfhwvzstVw+Y="
	vectorAttempt    = "00ff00ff00ff00ff00ff00ff00ff00ff"
	vectorNonce      = "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff"
	vectorSignature  = "8dd1a60c74bc099327bd1e9c48054df7fd168e681d6dffc8430adaa72179cd13485a905cfa7eff50dbca2c05f19664adf04fe08cf9e41c8ff234a4b60b7c74bb296bf391607de67ff265d5e3a26bcbf928797f22b95bdb6b00bf5d9249d5db972320618bb17ff737a61b3e27b9b176aaf9b620c041096b5c768e09bb1c0ab4d5c958ce7071020431ee3fc15929900194e1bd0c269ad5e5bf25be04bfd5c34bf9380d8debd374e0e1908f98530a7e5d170182b8744e67108756f9b53df31251a162e09a29c1ed5fff457e22e52b968cf85e9bea6715dd5172007003c663e7da11775ae8f63a7ba0114a9cc2e730834149ab1a3cef3ddefc91fdc3732cc5e634ca"
)

func TestEnrollmentProofAcceptsNativeSignerVector(t *testing.T) {
	der, err := base64.StdEncoding.DecodeString(vectorCertDERB64)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	if err := VerifyEnrollmentProof(certPEM, vectorAttempt, vectorNonce, vectorSignature); err != nil {
		t.Fatalf("native signer vector rejected: %v", err)
	}
	if err := VerifyEnrollmentProof(certPEM, vectorAttempt, vectorNonce, vectorSignature[:len(vectorSignature)-2]+"00"); err == nil {
		t.Fatal("altered signature accepted")
	}
}
