package link

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func TestRegisteredConnectorDetectsHalfOpenControlSession(t *testing.T) {
	client, server := establishedTLSPipe(t)
	defer closeTLSPipe(server)
	connector := &Connector{
		config: ConnectorConfig{
			KeepAliveInterval: 20 * time.Millisecond,
		},
		streams: make(chan struct{}, 1),
	}
	started := time.Now()
	err := connector.runRegisteredRelay(context.Background(), &registeredRelay{
		candidate: RelayCandidate{Name: "blackhole"},
		conn:      client,
	})
	if err == nil || !strings.Contains(err.Error(), "disconnected") {
		t.Fatalf("half-open control error=%v", err)
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatalf("half-open detection took %s", time.Since(started))
	}
}

func TestRegisteredConnectorCancellationClosesControlSessionPromptly(t *testing.T) {
	client, server := establishedTLSPipe(t)
	defer closeTLSPipe(server)
	connector := &Connector{
		config: ConnectorConfig{
			KeepAliveInterval: time.Minute,
		},
		streams: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	err := connector.runRegisteredRelay(ctx, &registeredRelay{
		candidate: RelayCandidate{Name: "cancelled"},
		conn:      client,
	})
	if err == nil {
		t.Fatal("cancelled connector returned no error")
	}
	if time.Since(started) > 200*time.Millisecond {
		t.Fatalf("connector cancellation took %s", time.Since(started))
	}
}

func closeTLSPipe(conn *tls.Conn) {
	_ = conn.NetConn().Close()
}

func establishedTLSPipe(t *testing.T) (*tls.Conn, *tls.Conn) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "control.test"},
		DNSNames:     []string{"control.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		publicKey,
		privateKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	client := tls.Client(left, &tls.Config{
		MinVersion:         tls.VersionTLS13,
		ServerName:         "control.test",
		InsecureSkipVerify: true,
	})
	server := tls.Server(right, &tls.Config{
		MinVersion: tls.VersionTLS13,
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{der},
			PrivateKey:  privateKey,
		}},
	})
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- server.Handshake() }()
	go func() { errorsCh <- client.Handshake() }()
	for range 2 {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
	}
	return client, server
}
