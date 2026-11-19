package main

import (
	"context"
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
}

func registerLocalAgentFlags(set *flag.FlagSet) *localAgentFlags {
	return &localAgentFlags{
		contract: set.Bool("local-agent-contract", false, "show the fixed local Agent update contract and exit"),
		status:   set.Bool("local-agent-status", false, "inspect the supervised Agent version and exit"),
		stopped:  set.Bool("local-agent-stopped", false, "inspect installed history only; requires a stopped service and --local-agent-status"),
		check:    set.Bool("check-agent-release", false, "verify and preflight a signed local Agent without switching"),
		install:  set.Bool("install-agent-release", false, "install a signed local Agent through the running supervisor"),
		reserve:  set.Bool("reserve-agent-bootstrap", false, "reserve the running supervisor until stdin closes or the service stops"),
		manifest: set.String("agent-manifest", "", "local signed Agent manifest"),
		binary:   set.String("agent-binary", "", "local raw Agent executable to verify"),
		version:  set.String("agent-version", "", "expected signed Agent version"),
	}
}

func executeLocalAgent(set *flag.FlagSet, f *localAgentFlags, configPath, version string) (bool, error) {
	localNames := map[string]bool{
		"local-agent-contract": true, "local-agent-status": true, "local-agent-stopped": true,
		"check-agent-release": true, "install-agent-release": true, "agent-manifest": true,
		"agent-binary": true, "agent-version": true,
		"reserve-agent-bootstrap": true,
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
	set.Visit(func(o *flag.Flag) {
		allowed := false
		switch {
		case *f.contract:
			allowed = o.Name == "local-agent-contract"
		case *f.status:
			allowed = o.Name == "config" || o.Name == "local-agent-status" || o.Name == "local-agent-stopped"
		case *f.check || *f.install || *f.reserve:
			allowed = o.Name == "config" || o.Name == "check-agent-release" || o.Name == "install-agent-release" || o.Name == "reserve-agent-bootstrap" || o.Name == "agent-manifest" || o.Name == "agent-binary" || o.Name == "agent-version"
		}
		valid = valid && allowed
	})
	if !valid || !*f.contract && configPath == "" || (*f.check || *f.install || *f.reserve) && (*f.manifest == "" || *f.binary == "" || *f.version == "") {
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
	} else {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if *f.reserve {
			return true, agenthost.ReserveBootstrap(ctx, configPath, *f.manifest, *f.binary, *f.version, os.Stdin, os.Stdout)
		}
		status, err = agenthost.LocalRelease(ctx, configPath, *f.manifest, *f.binary, *f.version, *f.install)
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
	return true, err
}
