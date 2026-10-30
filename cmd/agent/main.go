package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"proxycontrol/internal/agent/configuration"
	"syscall"

	"proxycontrol/internal/agent/agenthost"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/agent/hostnet"
	"proxycontrol/internal/agent/tuning"
	"proxycontrol/internal/platform/logging"
	"proxycontrol/pkg/contracts/agentstorage"
)

var version = "dev"

// agentFlags is the complete Agent command line. It stays deliberately small:
// the operator modes plus the supervisor, managed-worker and image-probe flags
// the Agent self-update path itself invokes.
type agentFlags struct {
	path              *string
	check             *bool
	showVersion       *bool
	describeStorage   *bool
	storageVersion    *int
	describeControl   *bool
	applyHostNetwork  *string
	applyTuning       *string
	managed           *bool
	supervise         *bool
	initializeHistory *bool
	enroll            *bool
	tokenFile         *string
	renewCertificate  *bool
	rotateKey         *bool
}

func registerFlags(set *flag.FlagSet) *agentFlags {
	f := &agentFlags{}
	f.path = set.String("config", "", "YAML configuration file")
	f.check = set.Bool("check-config", false, "validate configuration and exit")
	f.showVersion = set.Bool("version", false, "show version")
	f.describeStorage = set.Bool("storage-contract", false, "show fixed storage compatibility contract and exit")
	f.storageVersion = set.Int("storage-contract-version", 1, "storage descriptor query version (1 or 2)")
	f.describeControl = set.Bool("control-only-contract", false, "show fixed control-only worker capability and exit")
	f.applyHostNetwork = set.String("apply-host-network", "", "apply the enforced outbound source of a helper directory and exit")
	f.applyTuning = set.String("apply-tuning", "", "apply the fixed sysctl profile of a helper directory and exit")
	f.managed = set.Bool("managed-worker", false, "run with a local authenticated supervisor handshake")
	f.supervise = set.Bool("supervise", false, "run the authenticated installed Agent under supervision")
	f.initializeHistory = set.Bool("initialize-agent-history", false, "explicitly adopt this Agent as the initial supervised version")
	f.enroll = set.Bool("enroll", false, "enroll this node with a one-time token and exit")
	f.tokenFile = set.String("token-file", "", "file containing the one-time enrollment token")
	f.renewCertificate = set.Bool("renew-node-certificate", false, "renew the stopped node certificate and confirm its activation")
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

func execute(set *flag.FlagSet, f *agentFlags) error {
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
	if *f.enroll {
		valid := *f.path != "" && *f.tokenFile != "" && set.NArg() == 0
		set.Visit(func(o *flag.Flag) {
			if o.Name != "config" && o.Name != "enroll" && o.Name != "token-file" {
				valid = false
			}
		})
		if !valid {
			return fmt.Errorf("enrollment requires only --config, --enroll and --token-file")
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
	if *f.supervise || *f.initializeHistory {
		valid := *f.supervise && *f.path != "" && set.NArg() == 0
		set.Visit(func(o *flag.Flag) {
			if o.Name != "supervise" && o.Name != "initialize-agent-history" && o.Name != "config" {
				valid = false
			}
		})
		if !valid {
			return fmt.Errorf("supervision requires --supervise and --config, with optional first-time --initialize-agent-history")
		}
	}
	if *f.showVersion {
		fmt.Println("proxy-agent", version)
		return nil
	}
	logOutput := os.Stdout
	if *f.managed {
		logOutput = os.Stderr
	}
	logger := logging.New(logOutput)
	slog.SetDefault(logger)
	c, err := configuration.LoadAgent(*f.path)
	if err != nil {
		return err
	}
	if *f.check {
		logger.Info("configuration valid")
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *f.managed {
		return runManaged(ctx, c, logger)
	}
	active, err := agenthost.Active(c.DataDirectory)
	if err != nil {
		return err
	}
	if *f.supervise || active {
		if *f.tokenFile != "" {
			return fmt.Errorf("cannot enroll over supervised Agent state")
		}
		return agenthost.Run(ctx, *f.path, version, *f.initializeHistory, logger)
	}
	return control.Run(ctx, c, logger, version)
}
