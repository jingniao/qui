package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"proxycontrol/internal/agent/agenthost"
)

type localAgentFlags struct {
	contract, status, stopped, check, install, reserve *bool
	manifest, binary, version                          *string
	requestID, receipt                                 *string
}

func registerLocalAgentFlags(set *flag.FlagSet) *localAgentFlags {
	return &localAgentFlags{
		contract:  set.Bool("local-agent-contract", false, "show the fixed local Agent update contract and exit"),
		status:    set.Bool("local-agent-status", false, "inspect the supervised Agent version and exit"),
		stopped:   set.Bool("local-agent-stopped", false, "inspect installed history only; requires a stopped service and --local-agent-status"),
		check:     set.Bool("check-agent-release", false, "verify and preflight a signed local Agent without switching"),
		install:   set.Bool("install-agent-release", false, "install a signed local Agent through the running supervisor"),
		reserve:   set.Bool("reserve-agent-bootstrap", false, "reserve the running supervisor until stdin closes or the service stops"),
		manifest:  set.String("agent-manifest", "", "local signed Agent manifest"),
		binary:    set.String("agent-binary", "", "local raw Agent executable to verify"),
		version:   set.String("agent-version", "", "expected signed Agent version"),
		requestID: set.String("agent-install-request-id", "", "bind the supervised installation to this durable local request ID"),
		receipt:   set.String("agent-install-receipt", "", "read the original supervised installation receipt for this ID"),
	}
}

func executeLocalAgent(set *flag.FlagSet, f *localAgentFlags, configPath, version string) (bool, error) {
	localNames := map[string]bool{
		"local-agent-contract": true, "local-agent-status": true, "local-agent-stopped": true,
		"check-agent-release": true, "install-agent-release": true, "agent-manifest": true,
		"agent-binary": true, "agent-version": true,
		"reserve-agent-bootstrap":  true,
		"agent-install-request-id": true, "agent-install-receipt": true,
	}
	used := false
	set.Visit(func(o *flag.Flag) { used = used || localNames[o.Name] })
	if !used {
		return false, nil
	}
	count := 0
	for _, enabled := range []bool{*f.contract, *f.status, *f.check, *f.install, *f.reserve} {
		if enabled {
			count++
		}
	}
	valid := count == 1 && set.NArg() == 0
	if *f.receipt != "" {
		count++
		valid = count == 1 && set.NArg() == 0
	}
	set.Visit(func(o *flag.Flag) {
		allowed := false
		switch {
		case *f.receipt != "":
			allowed = o.Name == "config" || o.Name == "agent-install-receipt" || o.Name == "agent-manifest"
		case *f.contract:
			allowed = o.Name == "local-agent-contract"
		case *f.status:
			allowed = o.Name == "config" || o.Name == "local-agent-status" || o.Name == "local-agent-stopped"
		case *f.check || *f.install || *f.reserve:
			allowed = o.Name == "config" || o.Name == "check-agent-release" || o.Name == "install-agent-release" || o.Name == "reserve-agent-bootstrap" || o.Name == "agent-manifest" || o.Name == "agent-binary" || o.Name == "agent-version"
			allowed = allowed || *f.install && o.Name == "agent-install-request-id"
		}
		valid = valid && allowed
	})
	if !valid || !*f.contract && configPath == "" || *f.receipt != "" && *f.manifest == "" || (*f.check || *f.install || *f.reserve) && (*f.manifest == "" || *f.binary == "" || *f.version == "") {
		return true, errors.New("local Agent operation requires exactly one mode and only its documented flags; release checks require config, agent-manifest, agent-binary and agent-version")
	}
	if runtime.GOOS != "linux" {
		return true, errors.New("local Agent updates require Linux")
	}
	if *f.contract {
		fmt.Printf("proxy-agent-local-v1 %s\n", version)
		return true, nil
	}
	var status agenthost.LocalAgentStatus
	var err error
	if *f.status {
		status, err = agenthost.LocalStatus(configPath, *f.stopped)
	} else if *f.receipt != "" {
		status, err = agenthost.LocalReceipt(context.Background(), configPath, *f.receipt, *f.manifest)
	} else {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if *f.reserve {
			return true, agenthost.ReserveBootstrap(ctx, configPath, *f.manifest, *f.binary, *f.version, os.Stdin, os.Stdout)
		}
		if *f.requestID != "" {
			status, err = agenthost.LocalRelease(ctx, configPath, *f.manifest, *f.binary, *f.version, *f.install, *f.requestID)
		} else {
			status, err = agenthost.LocalRelease(ctx, configPath, *f.manifest, *f.binary, *f.version, *f.install)
		}
	}
	if status.Current.Version != "" {
		state, readiness := "installed", "busy"
		if status.Running {
			state = "running"
		}
		if status.Ready {
			readiness = "ready"
		} else if status.Recovery {
			readiness = "local-recovery"
		} else if status.Running && status.Revision == 0 {
			readiness = "baseline-required"
		}
		fmt.Printf("proxy-agent-local-v1 %s %s %s\n", state, status.Current.Version, readiness)
	} else if *f.status && err == nil {
		fmt.Println("proxy-agent-local-v1 unmanaged - idle")
	}
	if status.Receipt != nil {
		r := status.Receipt
		state, phase := "pending", "absent"
		if r.Result != nil {
			state = r.Result.Status
		}
		if r.Installation != nil {
			phase = r.Installation.Phase
		}
		fmt.Printf("proxy-agent-install-receipt-v1 %s %s %d %s %s %s %s\n", r.Request.ID, r.Request.NodeID, r.Request.Revision, r.Request.Release.Manifest.Version, r.Request.Release.Manifest.SHA256, state, phase)
		status.Installation = r.Outcome
	}
	if (*f.install || *f.receipt != "") && status.Installation != nil {
		original, encodeErr := json.Marshal(status.Installation.OriginalError)
		if encodeErr != nil {
			return true, errors.Join(err, encodeErr)
		}
		recovery, encodeErr := json.Marshal(status.Installation.Recovery)
		if encodeErr != nil {
			return true, errors.Join(err, encodeErr)
		}
		fmt.Printf("proxy-agent-install-original-error-v1 %s\nproxy-agent-install-recovery-v1 %s\n", original, recovery)
	}
	return true, err
}
