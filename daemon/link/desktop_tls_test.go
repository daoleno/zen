package link

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestSingleConnectionPreservesOnlyActualTLS(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprint(encrypted), func(t *testing.T) {
			var client, server net.Conn
			if encrypted {
				client, server = establishedTLSPipe(t)
			} else {
				client, server = net.Pipe()
			}
			defer client.Close()
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			observed := make(chan bool, 1)
			done := make(chan error, 1)
			go func() {
				done <- serveSingleConnection(ctx, server, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					observed <- r.TLS != nil && r.TLS.HandshakeComplete && r.TLS.Version == tls.VersionTLS13
					w.WriteHeader(http.StatusNoContent)
				}), time.Second)
			}()
			_ = client.SetDeadline(time.Now().Add(2 * time.Second))
			_, err := io.WriteString(client, "GET /desktop HTTP/1.1\r\nHost: host.test\r\nX-Forwarded-Proto: https\r\nForwarded: proto=https\r\nConnection: close\r\n\r\n")
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(client), nil)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if got := <-observed; got != encrypted {
				t.Fatalf("TLS provenance = %v, want %v", got, encrypted)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("connection server did not stop")
			}
		})
	}
}
