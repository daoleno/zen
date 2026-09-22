package main

import (
	"context"
	"flag"
	"github.com/daoleno/zen/daemon/work"
	"os"
	"os/signal"
	"syscall"
)

func runDSHSessionCLI(args []string) error {
	flags := flag.NewFlagSet("dsh-session", flag.ContinueOnError)
	id := flags.String("dsh-session", "", "Exact native DSH session identity")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	return work.RunDSHSession(ctx, *id, cwd)
}
