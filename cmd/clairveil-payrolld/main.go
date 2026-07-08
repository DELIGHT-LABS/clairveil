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
	var planPath string
	var txQueryPath string
	var nullifiersPath string
	var mode string
	var once bool
	var interval time.Duration
	var leaseOwner string
	var leaseTTL time.Duration
	var maxOperations int
	var outPath string
	flags.StringVar(&statePath, "state", "", "durable reservation state JSON path")
	flags.StringVar(&planPath, "plan", "", "payroll plan JSON path for live mode")
	flags.StringVar(&txQueryPath, "tx-query", "", "clairveild query tx JSON path or TxObservation JSON path for live mode")
	flags.StringVar(&nullifiersPath, "nullifiers", "", "optional nullifier status JSON path for live mode")
	flags.StringVar(&mode, "mode", "simulated", "daemon mode: simulated|live")
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
	if mode != "simulated" && mode != "live" {
		return fmt.Errorf("unsupported mode %q; supported modes: simulated|live", mode)
	}
	if interval <= 0 {
		return fmt.Errorf("-interval must be positive")
	}

	runner, err := buildDaemonRunner(mode, statePath, planPath, txQueryPath, nullifiersPath, leaseOwner, leaseTTL, maxOperations)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if once {
		report, err := runner.RunOnce(ctx)
		if err != nil {
			return err
		}
		return writeJSON(outPath, report)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		report, err := runner.RunOnce(ctx)
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

type daemonRunner interface {
	RunOnce(context.Context) (*privacypayroll.ReferenceDaemonRunReport, error)
}

type reopeningDaemonRunner struct {
	mode           string
	statePath      string
	planPath       string
	txQueryPath    string
	nullifiersPath string
	leaseOwner     string
	leaseTTL       time.Duration
	maxOperations  int
}

func (r reopeningDaemonRunner) RunOnce(ctx context.Context) (*privacypayroll.ReferenceDaemonRunReport, error) {
	if err := requireExistingStatePath(r.statePath); err != nil {
		return nil, err
	}
	store, err := privacyreservation.OpenDurableFileStore(r.statePath)
	if err != nil {
		return nil, err
	}
	runner, err := buildDaemonRunnerForStore(r.mode, store, r.planPath, r.txQueryPath, r.nullifiersPath, r.leaseOwner, r.leaseTTL, r.maxOperations)
	if err != nil {
		return nil, err
	}
	return runner.RunOnce(ctx)
}

func buildDaemonRunner(mode string, statePath string, planPath string, txQueryPath string, nullifiersPath string, leaseOwner string, leaseTTL time.Duration, maxOperations int) (daemonRunner, error) {
	if err := requireExistingStatePath(statePath); err != nil {
		return nil, err
	}
	if _, err := privacyreservation.OpenDurableFileStore(statePath); err != nil {
		return nil, err
	}
	if mode == "live" {
		if _, err := readPayrollPlan(planPath); err != nil {
			return nil, err
		}
		if strings.TrimSpace(txQueryPath) == "" {
			return nil, fmt.Errorf("-tx-query is required in live mode")
		}
	}
	return reopeningDaemonRunner{
		mode:           mode,
		statePath:      statePath,
		planPath:       planPath,
		txQueryPath:    txQueryPath,
		nullifiersPath: nullifiersPath,
		leaseOwner:     leaseOwner,
		leaseTTL:       leaseTTL,
		maxOperations:  maxOperations,
	}, nil
}

func requireExistingStatePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("-state is required")
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("durable reservation state %s does not exist; run clairveil-payroll run first", path)
		}
		return err
	}
	return nil
}

func buildDaemonRunnerForStore(mode string, store privacyreservation.Store, planPath string, txQueryPath string, nullifiersPath string, leaseOwner string, leaseTTL time.Duration, maxOperations int) (daemonRunner, error) {
	reservationService := privacyreservation.Service{Store: store}
	switch mode {
	case "simulated":
		return privacypayroll.ReferenceDaemon{
			Reservation:   reservationService,
			LeaseOwner:    leaseOwner,
			LeaseTTL:      leaseTTL,
			MaxOperations: maxOperations,
		}, nil
	case "live":
		plan, err := readPayrollPlan(planPath)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(txQueryPath) == "" {
			return nil, fmt.Errorf("-tx-query is required in live mode")
		}
		return privacypayroll.LiveDaemon{
			Reservation:   reservationService,
			Executor:      fileEvidenceExecutor{store: store, plan: *plan, txQueryPath: txQueryPath, nullifiersPath: nullifiersPath},
			LeaseOwner:    leaseOwner,
			LeaseTTL:      leaseTTL,
			MaxOperations: maxOperations,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported mode %q", mode)
	}
}

type fileEvidenceExecutor struct {
	store          privacyreservation.Store
	plan           privacypayroll.PayrollPlan
	txQueryPath    string
	nullifiersPath string
}

func (e fileEvidenceExecutor) BuildProofReady(context.Context, privacypayroll.LiveOperationGroup) (privacyreservation.ProofReadyOperationUpdate, string, error) {
	return privacyreservation.ProofReadyOperationUpdate{}, "proof generation is handled by an external live worker", privacypayroll.ErrLiveDaemonSkip
}

func (e fileEvidenceExecutor) BroadcastProofReady(context.Context, privacypayroll.LiveOperationGroup) (*privacypayroll.BroadcastResult, string, error) {
	return nil, "broadcast is handled by an external live worker", privacypayroll.ErrLiveDaemonSkip
}

func (e fileEvidenceExecutor) ScanSubmitted(ctx context.Context, group privacypayroll.LiveOperationGroup) (map[string]privacyreservation.OperationEvidence, string, error) {
	tx, err := readTxObservation(e.txQueryPath)
	if err != nil {
		return nil, "", err
	}
	nullifiers, err := readNullifierStatuses(e.nullifiersPath)
	if err != nil {
		return nil, "", err
	}
	report, err := (privacypayroll.EvidenceScanner{Store: e.store}).ScanTransferBatch(ctx, e.plan, tx, nullifiers)
	if err != nil {
		return nil, "", err
	}
	out := make(map[string]privacyreservation.OperationEvidence)
	for _, item := range report.Evidence {
		if item.OperationID != group.Operation.OperationID {
			continue
		}
		out[item.ReservationID] = item.Evidence
	}
	if len(out) == 0 {
		return nil, "no scanned evidence matched operation", privacypayroll.ErrLiveDaemonSkip
	}
	return out, "scanned transfer-batch tx evidence", nil
}

func readPayrollPlan(path string) (*privacypayroll.PayrollPlan, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("-plan is required in live mode")
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plan privacypayroll.PayrollPlan
	if err := json.Unmarshal(bz, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func readTxObservation(path string) (privacypayroll.TxObservation, error) {
	if strings.TrimSpace(path) == "" {
		return privacypayroll.TxObservation{}, fmt.Errorf("-tx-query is required in live mode")
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return privacypayroll.TxObservation{}, err
	}
	return privacypayroll.ParseTxObservationJSON(bz)
}

func readNullifierStatuses(path string) ([]privacypayroll.NullifierStatus, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Statuses []privacypayroll.NullifierStatus `json:"statuses"`
	}
	if err := json.Unmarshal(bz, &wrapped); err == nil && len(wrapped.Statuses) > 0 {
		return wrapped.Statuses, nil
	}
	var statuses []privacypayroll.NullifierStatus
	if err := json.Unmarshal(bz, &statuses); err == nil && len(statuses) > 0 {
		return statuses, nil
	}
	return nil, fmt.Errorf("nullifier status file is empty or invalid")
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
