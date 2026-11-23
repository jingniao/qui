// Go 1.24 and later open listening sockets as MPTCP by default on Linux; the
// remote root exploit for the kernel bug CVE-2026-98432 needs an MPTCP listener.

//go:debug multipathtcp=0
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"proxycontrol/internal/agent/configuration"
	"syscall"

	"proxycontrol/internal/agent/agenthost"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/agent/hostnet"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/lifecycle"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/tuning"
	"proxycontrol/internal/platform/logging"
	"proxycontrol/pkg/contracts/agentstorage"
)

var version = "dev"

// agentFlags is the complete Agent command line. It stays deliberately small:
// the operator modes plus the supervisor, managed-worker and image-probe flags
// the Agent self-update path itself invokes.
type agentFlags struct {
	reset                *stateResetFlags
	handover             *handoverFlags
	local                *localAgentFlags
	path                 *string
	check                *bool
	showVersion          *bool
	systemUpdateIdentity *bool
	describeStorage      *bool
	storageVersion       *int
	describeControl      *bool
	applyHostNetwork     *string
	applyTuning          *string
	probeTuning          *string
	installTuningModules *bool
	managed              *bool
	supervise            *bool
	initializeHistory    *bool
	enroll               *bool
	tokenFile            *string
	renewCertificate     *bool
	rotateKey            *bool
	drainTelemetry       *bool
}

func registerFlags(set *flag.FlagSet) *agentFlags {
	f := &agentFlags{}
	f.reset = registerStateResetFlags(set)
	f.handover = registerHandoverFlags(set)
	f.local = registerLocalAgentFlags(set)
	f.path = set.String("config", "", "YAML configuration file")
	f.check = set.Bool("check-config", false, "validate configuration and exit")
	f.showVersion = set.Bool("version", false, "show version")
	f.systemUpdateIdentity = set.Bool("system-update-identity", false, "read the enrolled node identity and authenticated configuration revision")
	f.describeStorage = set.Bool("storage-contract", false, "show fixed storage compatibility contract and exit")
	f.storageVersion = set.Int("storage-contract-version", 1, "storage descriptor query version (1 or 2)")
	f.describeControl = set.Bool("control-only-contract", false, "show fixed control-only worker capability and exit")
	f.applyHostNetwork = set.String("apply-host-network", "", "apply the enforced outbound source of a helper directory and exit")
	f.applyTuning = set.String("apply-tuning", "", "apply the fixed sysctl profile of a helper directory and exit")
	f.probeTuning = set.String("probe-tuning", "", "probe write access to the fixed sysctl whitelist and exit")
	f.installTuningModules = set.Bool("install-tuning-modules", false, "process the fixed Ubuntu BBR/fq module request and exit")
	f.managed = set.Bool("managed-worker", false, "run with a local authenticated supervisor handshake")
	f.supervise = set.Bool("supervise", false, "run the authenticated installed Agent under supervision")
	f.initializeHistory = set.Bool("initialize-agent-history", false, "explicitly adopt this Agent as the initial supervised version")
	f.enroll = set.Bool("enroll", false, "enroll this node with a one-time token and exit")
	f.tokenFile = set.String("token-file", "", "file containing the one-time enrollment token")
	f.renewCertificate = set.Bool("renew-node-certificate", false, "renew the stopped node certificate and confirm its activation")
	f.drainTelemetry = set.Bool("drain-telemetry", false, "upload stopped Agent telemetry for a bound replacement and exit")
	f.rotateKey = set.Bool("rotate-node-key", false, "rotate the stopped node private key and confirm its certificate")
	return f
}

func main() {
	if err := run(); err != nil {
		slog.Error("agent failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	set := flag.CommandLine
	f := registerFlags(set)
	set.Parse(os.Args[1:])
	return execute(set, f)
}

func execute(set *flag.FlagSet, f *agentFlags) (resultErr error) {
	if handled, err := executeStateReset(set, f.reset, *f.path, version); handled {
		return err
	}
	if handled, err := executeHandover(set, f.handover, *f.path); handled {
		return err
	}
	if *f.systemUpdateIdentity {
		valid := *f.path != "" && set.NArg() == 0
		set.Visit(func(o *flag.Flag) { valid = valid && (o.Name == "config" || o.Name == "system-update-identity") })
		if !valid {
			return errors.New("--system-update-identity requires only --config")
		}
		c, err := configuration.LoadAgent(*f.path)
		if err != nil {
			return err
		}
		node, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
		if err != nil {
			return err
		}
		key, err := os.ReadFile(filepath.Join(c.DataDirectory, "configuration.key"))
		if err != nil {
			return err
		}
		defer clear(key)
		if len(key) != 32 {
			return errors.New("configuration key must contain exactly 32 bytes")
		}
		revision, err := reconcile.InspectCurrent(c.DataDirectory, node.NodeID, key)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			NodeID   string `json:"nodeId"`
			Revision int64  `json:"revision"`
		}{node.NodeID, revision})
	}
	if *f.installTuningModules {
		if set.NArg() != 0 || set.NFlag() != 1 {
			return errors.New("--install-tuning-modules must be used alone")
		}
		return tuning.RunModuleHelper()
	}
	if handled, err := executeLocalAgent(set, f.local, *f.path, version); handled {
		return err
	}
	if *f.probeTuning != "" {
		if set.NArg() != 0 || set.NFlag() != 1 {
			return fmt.Errorf("--probe-tuning cannot be combined with other flags")
		}
		return tuning.RunProbe(*f.probeTuning)
	}
	if *f.applyTuning != "" {
		exclusive := set.NArg() == 0
		set.Visit(func(o *flag.Flag) {
			if o.Name != "apply-tuning" {
				exclusive = false
			}
		})
		if !exclusive {
			return fmt.Errorf("--apply-tuning cannot be combined with other flags")
		}
		return tuning.RunHelper(*f.applyTuning)
	}
	// The enforced-source helper is a fixed operation of the packaged unit: it
	// reads one validated file and runs exactly one program, so it never loads a
	// configuration and never accepts another flag.
	if *f.applyHostNetwork != "" {
		exclusive := set.NArg() == 0
		set.Visit(func(o *flag.Flag) {
			if o.Name != "apply-host-network" {
				exclusive = false
			}
		})
		if !exclusive {
			return fmt.Errorf("--apply-host-network cannot be combined with other flags")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return hostnet.RunHelper(ctx, *f.applyHostNetwork)
	}
	if *f.renewCertificate || *f.rotateKey {
		operations := 0
		for _, enabled := range []bool{*f.renewCertificate, *f.rotateKey} {
			if enabled {
				operations++
			}
		}
		valid := *f.path != "" && operations == 1 && set.NArg() == 0
		set.Visit(func(o *flag.Flag) {
			if o.Name != "config" && o.Name != "renew-node-certificate" && o.Name != "rotate-node-key" {
				valid = false
			}
		})
		if !valid {
			return fmt.Errorf("certificate operation requires only config and exactly one certificate operation flag")
		}
		c, err := configuration.LoadAgent(*f.path)
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		var operationErr error
		if *f.rotateKey {
			operationErr = control.RotateNodeKeyOnly(ctx, c, version)
		} else {
			operationErr = control.RenewCertificateOnly(ctx, c)
		}
		if operationErr != nil {
			return operationErr
		}
		fmt.Println("node certificate activation confirmed")
		return nil
	}
	// Enrollment is a terminal operator mode: it enrolls this node and exits
	// without starting the control worker. The systemd unit never passes it.
	if *f.drainTelemetry && *f.check {
		valid := *f.path != "" && set.NArg() == 0
		set.Visit(func(o *flag.Flag) {
			if o.Name != "config" && o.Name != "drain-telemetry" && o.Name != "check-config" {
				valid = false
			}
		})
		if !valid {
			return errors.New("replacement preflight requires only --config, --drain-telemetry and --check-config")
		}
		c, err := configuration.LoadAgent(*f.path)
		if err != nil {
			return err
		}
		return control.CheckReplacementConfig(c)
	}
	if *f.enroll || *f.drainTelemetry {
		operation := "enroll"
		if *f.drainTelemetry {
			operation = "drain-telemetry"
		}
		valid := *f.path != "" && *f.tokenFile != "" && set.NArg() == 0
		set.Visit(func(o *flag.Flag) {
			if o.Name != "config" && o.Name != operation && o.Name != "token-file" {
				valid = false
			}
		})
		if !valid {
			if operation == "enroll" {
				return errors.New("enrollment requires only --config, --enroll and --token-file")
			}
			return fmt.Errorf("%s requires only --config, --%s and --token-file", operation, operation)
		}
		token, err := readEnrollmentToken(*f.tokenFile)
		if err != nil {
			return err
		}
		c, err := configuration.LoadAgent(*f.path)
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if *f.drainTelemetry {
			return control.DrainTelemetry(ctx, c, token, version, *f.tokenFile, func(ctx context.Context) error {
				if _, err := fmt.Fprintln(os.Stdout, "old-agent-drained"); err != nil {
					return err
				}
				detached := make(chan error, 1)
				go func() {
					var receipt string
					_, err := fmt.Fscanln(os.Stdin, &receipt)
					if err == nil && receipt != "detached" {
						err = errors.New("installer did not detach the confirmed data directory")
					}
					detached <- err
				}()
				select {
				case err := <-detached:
					return err
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}
		if err := control.Enroll(ctx, c, token, version); err != nil {
			return err
		}
		fmt.Println("node enrollment completed")
		return nil
	}
	if *f.describeControl {
		exclusive := set.NArg() == 0
		set.Visit(func(o *flag.Flag) {
			if o.Name != "control-only-contract" {
				exclusive = false
			}
		})
		if !exclusive {
			return fmt.Errorf("--control-only-contract cannot be combined with other flags")
		}
		fmt.Printf("proxy-agent-control-only-v1 %s\n", version)
		return nil
	}
	storageVersionExplicit := false
	set.Visit(func(o *flag.Flag) {
		if o.Name == "storage-contract-version" {
			storageVersionExplicit = true
		}
	})
	if storageVersionExplicit && !*f.describeStorage {
		return fmt.Errorf("--storage-contract-version requires --storage-contract")
	}
	if *f.describeStorage {
		exclusive := true
		set.Visit(func(o *flag.Flag) {
			if o.Name != "storage-contract" && o.Name != "storage-contract-version" {
				exclusive = false
			}
		})
		if !exclusive || set.NArg() != 0 || *f.storageVersion < 1 || *f.storageVersion > 2 {
			return fmt.Errorf("--storage-contract cannot be combined with configuration or runtime flags")
		}
		contract := agentstorage.Current(version)
		if *f.storageVersion == 1 {
			contract.AgentRecoveryProtocol = 0
		}
		if err := contract.Validate(); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(contract)
	}
	if *f.managed {
		valid := *f.path != "" && set.NArg() == 0
		set.Visit(func(o *flag.Flag) {
			if o.Name != "managed-worker" && o.Name != "config" {
				valid = false
			}
		})
		if !valid {
			return fmt.Errorf("managed worker requires only --config and its local handshake")
		}
	}
	if *f.initializeHistory {
		valid := !*f.supervise && *f.path != "" && set.NArg() == 0
		set.Visit(func(o *flag.Flag) {
			if o.Name != "initialize-agent-history" && o.Name != "config" {
				valid = false
			}
		})
		if !valid {
			return fmt.Errorf("initialization requires only --config and --initialize-agent-history")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := agenthost.Initialize(ctx, *f.path, version); err != nil {
			return err
		}
		fmt.Println("agent web upgrade enabled")
		return nil
	}
	if *f.showVersion {
		fmt.Println("proxy-agent", version)
		return nil
	}
	c, err := configuration.LoadAgent(*f.path)
	if err != nil {
		return err
	}
	if *f.check {
		logging.New(os.Stdout).Info("configuration valid")
		return nil
	}
	var logOutput io.Writer = os.Stderr
	var workerLogs io.Writer = os.Stderr
	if !*f.managed {
		logs, err := openEnrolledLogs(c.DataDirectory)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, logs.Close()) }()
		logOutput = logs
		workerLogs = logs.WorkerOutput()
	}
	logger := logging.New(logOutput)
	slog.SetDefault(logger)
	defer slog.SetDefault(logging.New(os.Stderr))
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	defer cancel(control.ErrOrdinaryStop)
	go func() {
		select {
		case sig := <-signals:
			if sig == syscall.SIGTERM {
				cancel(control.ErrTerminationRequested)
			} else {
				cancel(control.ErrOrdinaryStop)
			}
		case <-ctx.Done():
		}
	}()
	if *f.managed {
		return runManaged(ctx, c, logger)
	}
	if cleaned, cleanErr := agenthost.CleanupInterrupted(c.DataDirectory); cleanErr != nil {
		return cleanErr
	} else if cleaned {
		logger.Warn("interrupted Agent web-upgrade initialization was cleaned up")
	}
	active, err := agenthost.Active(c.DataDirectory)
	if err != nil {
		return err
	}
	if *f.supervise || active {
		if *f.tokenFile != "" {
			return fmt.Errorf("cannot enroll over supervised Agent state")
		}
		return agenthost.Run(ctx, *f.path, version, logger, workerLogs)
	}
	return control.Run(ctx, c, logger, version)
}

func openEnrolledLogs(directory string) (logs *logging.RotatingWriter, resultErr error) {
	supervisor, err := lifecycle.TrySupervisor(directory)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, supervisor.Close())
		if resultErr != nil && logs != nil {
			resultErr = errors.Join(resultErr, logs.Close())
			logs = nil
		}
	}()
	owner, err := lifecycle.Try(directory)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	if _, err := identity.Load(filepath.Join(owner.Directory(), "identity")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("node is not enrolled; use --enroll --token-file with its one-time enrollment code")
		}
		return nil, err
	}
	return logging.OpenAgentLogs(filepath.Join(owner.Directory(), "logs"), func(err error) {
		logging.New(os.Stderr).Error("Agent file logging failed; runtime logs unavailable until restart", "error", err)
	})
}
