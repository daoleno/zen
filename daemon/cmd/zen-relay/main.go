package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/zen/daemon/linkproto"
	"github.com/daoleno/zen/daemon/relay"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("zen-relay: %v", err)
	}
}

func run(args []string) error {
	defaults := relay.DefaultConfig("")
	var clientAddress string
	var controlAddress string
	var operatorAddress string
	var certificateFile string
	var keyFile string
	var tokenEnvironment string
	var maxRoutes int
	var maxClients int
	var maxClientsPerRoute int
	var maxClientHandshakes int
	var maxConnectorHandshakes int
	var maxAdmissions int
	var maxAdmissionsPerRoute int
	var maxNonces int
	var maxPendingStreams int
	var handshakeTimeout time.Duration
	var attachTimeout time.Duration
	var idleTimeout time.Duration
	var sweepInterval time.Duration
	var checkURL string

	flags := flag.NewFlagSet("zen-relay", flag.ContinueOnError)
	flags.StringVar(&clientAddress, "client-addr", ":443", "raw L4 client listen address")
	flags.StringVar(&controlAddress, "control-addr", ":8443", "TLS connector control listen address")
	flags.StringVar(&operatorAddress, "operator-addr", "127.0.0.1:8080", "health and metrics listen address")
	flags.StringVar(&certificateFile, "tls-cert", "", "PEM certificate for the connector control listener")
	flags.StringVar(&keyFile, "tls-key", "", "PEM private key for the connector control listener")
	flags.StringVar(&tokenEnvironment, "connector-token-env", "ZEN_LINK_CONNECTOR_TOKEN", "environment variable containing the connector token")
	flags.IntVar(&maxRoutes, "max-routes", defaults.MaxRoutes, "maximum concurrently registered routes")
	flags.IntVar(&maxClients, "max-clients", defaults.MaxClients, "maximum concurrent client streams")
	flags.IntVar(&maxClientsPerRoute, "max-clients-per-route", defaults.MaxClientsPerRoute, "maximum concurrent streams for one route")
	flags.IntVar(&maxClientHandshakes, "max-client-handshakes", defaults.MaxClientHandshakes, "maximum concurrent client routing handshakes")
	flags.IntVar(&maxConnectorHandshakes, "max-connector-handshakes", defaults.MaxConnectorHandshakes, "maximum concurrent connector TLS/auth handshakes")
	flags.IntVar(&maxAdmissions, "max-admissions", defaults.MaxAdmissions, "maximum unexpired one-time admissions")
	flags.IntVar(&maxAdmissionsPerRoute, "max-admissions-per-route", defaults.MaxAdmissionsPerRoute, "maximum unexpired admissions for one route")
	flags.IntVar(&maxNonces, "max-nonces", defaults.MaxNonces, "maximum retained connector replay nonces")
	flags.IntVar(&maxPendingStreams, "max-pending-streams", defaults.MaxPendingStreams, "maximum streams awaiting connector attachment")
	flags.DurationVar(&handshakeTimeout, "handshake-timeout", defaults.HandshakeTimeout, "maximum time to receive routing SNI or connector auth")
	flags.DurationVar(&attachTimeout, "attach-timeout", defaults.AttachTimeout, "maximum time for a connector to attach a stream")
	flags.DurationVar(&idleTimeout, "idle-timeout", defaults.IdleTimeout, "idle timeout only when neither stream direction makes progress")
	flags.DurationVar(&sweepInterval, "sweep-interval", defaults.SweepInterval, "expired control-state cleanup interval")
	flags.StringVar(&checkURL, "check", "", "check an existing relay health URL and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(checkURL) != "" {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get(checkURL)
		if err != nil {
			return fmt.Errorf("relay health check: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("relay health check returned %s", response.Status)
		}
		return nil
	}
	connectorToken := strings.TrimSpace(os.Getenv(strings.TrimSpace(tokenEnvironment)))
	if connectorToken == "" {
		return fmt.Errorf("connector token environment variable %s is empty", tokenEnvironment)
	}
	server, err := relay.New(relay.Config{
		ConnectorToken:         connectorToken,
		MaxRoutes:              maxRoutes,
		MaxClients:             maxClients,
		MaxClientsPerRoute:     maxClientsPerRoute,
		MaxClientHandshakes:    maxClientHandshakes,
		MaxConnectorHandshakes: maxConnectorHandshakes,
		MaxAdmissions:          maxAdmissions,
		MaxAdmissionsPerRoute:  maxAdmissionsPerRoute,
		MaxNonces:              maxNonces,
		MaxPendingStreams:      maxPendingStreams,
		HandshakeTimeout:       handshakeTimeout,
		AttachTimeout:          attachTimeout,
		IdleTimeout:            idleTimeout,
		AuthMaxAge:             defaults.AuthMaxAge,
		MaxAdmissionTTL:        defaults.MaxAdmissionTTL,
		SweepInterval:          sweepInterval,
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(certificateFile) == "" || strings.TrimSpace(keyFile) == "" {
		return errors.New("-tls-cert and -tls-key are required")
	}
	certificate, err := tls.LoadX509KeyPair(certificateFile, keyFile)
	if err != nil {
		return fmt.Errorf("load control TLS identity: %w", err)
	}

	clientListener, err := net.Listen("tcp", clientAddress)
	if err != nil {
		return fmt.Errorf("listen for Link clients: %w", err)
	}
	defer clientListener.Close()
	controlTCP, err := net.Listen("tcp", controlAddress)
	if err != nil {
		return fmt.Errorf("listen for Link connectors: %w", err)
	}
	defer controlTCP.Close()
	controlListener := tls.NewListener(controlTCP, &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		NextProtos:   []string{linkproto.ControlALPN},
	})

	operatorListener, err := net.Listen("tcp", operatorAddress)
	if err != nil {
		return fmt.Errorf("listen for operator probes: %w", err)
	}
	defer operatorListener.Close()
	operatorServer := &http.Server{
		Handler:           server.OperatorHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errorsCh := make(chan error, 2)
	go func() {
		errorsCh <- server.Serve(ctx, clientListener, controlListener)
	}()
	go func() {
		errorsCh <- operatorServer.Serve(operatorListener)
	}()

	log.Printf(
		"zen-relay ready: client=%s control=%s operator=%s protocol=%d",
		clientListener.Addr(),
		controlTCP.Addr(),
		operatorListener.Addr(),
		1,
	)
	select {
	case <-ctx.Done():
	case err := <-errorsCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) &&
			!errors.Is(err, net.ErrClosed) &&
			!errors.Is(err, context.Canceled) {
			stop()
			return err
		}
	}
	stop()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = operatorServer.Shutdown(shutdownContext)
	return nil
}
