package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	privacypayroll "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/payroll"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("clairveil-payrolld", flag.ContinueOnError)
	var statePath string
	var mode string
	var once bool
	var interval time.Duration
	var leaseOwner string
	var leaseTTL time.Duration
	var maxOperations int
	var outPath string
	flags.StringVar(&statePath, "state", "", "durable reservation state JSON path")
	flags.StringVar(&mode, "mode", "simulated", "daemon mode: simulated")
	flags.BoolVar(&once, "once", false, "run one scheduler tick and exit")
	flags.DurationVar(&interval, "interval", 5*time.Second, "scheduler tick interval")
	flags.StringVar(&leaseOwner, "lease-owner", "clairveil-payrolld", "reservation lease owner")
	flags.DurationVar(&leaseTTL, "lease-ttl", time.Minute, "reservation lease ttl")
	flags.IntVar(&maxOperations, "max-operations", 0, "maximum operations per tick; 0 means unlimited")
	flags.StringVar(&outPath, "out", "", "optional JSON report path for -once; stdout when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(statePath) == "" {
		return fmt.Errorf("-state is required")
	}
	if mode != "simulated" {
		return fmt.Errorf("unsupported mode %q; only simulated is currently available", mode)
	}
	if interval <= 0 {
		return fmt.Errorf("-interval must be positive")
	}

	store, err := privacyreservation.OpenDurableFileStore(statePath)
	if err != nil {
		return err
	}
	daemon := privacypayroll.ReferenceDaemon{
		Reservation:   privacyreservation.Service{Store: store},
		LeaseOwner:    leaseOwner,
		LeaseTTL:      leaseTTL,
		MaxOperations: maxOperations,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if once {
		report, err := daemon.RunOnce(ctx)
		if err != nil {
			return err
		}
		return writeJSON(outPath, report)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		report, err := daemon.RunOnce(ctx)
		if err != nil {
			return err
		}
		if err := writeJSON("", report); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func writeJSON(path string, value any) error {
	bz, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	bz = append(bz, '\n')
	if strings.TrimSpace(path) == "" {
		_, err = os.Stdout.Write(bz)
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(path, bz, 0o600)
}
