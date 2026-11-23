package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"proxycontrol/internal/agent/handovercli"
)

type handoverFlags struct {
	operations                                  map[string]*bool
	scope, transaction, target, request, reason *string
	updater                                     *int
}

func registerHandoverFlags(set *flag.FlagSet) *handoverFlags {
	f := &handoverFlags{operations: map[string]*bool{}}
	for _, name := range []string{"core-handover-protocol", "core-handover-target", "core-handover-arm", "core-handover-status", "core-handover-revoke", "core-handover-revoke-all", "finish-core-stop"} {
		f.operations[name] = set.Bool(name, false, "local root core handover operation")
	}
	f.scope = set.String("scope", "", "handover scope: bootstrap or worker")
	f.transaction = set.String("transaction", "", "local update transaction ID")
	f.target = set.String("target-sha256", "", "actual target Agent image SHA-256")
	f.updater = set.Int("updater-pid", 0, "live local updater PID")
	f.request = set.String("request", "", "handover request ID")
	f.reason = set.String("reason", "", "revocation reason")
	return f
}

func executeHandover(set *flag.FlagSet, f *handoverFlags, configPath string) (bool, error) {
	operation := ""
	count := 0
	for name, enabled := range f.operations {
		if *enabled {
			operation = name
			count++
		}
	}
	auxiliary := false
	set.Visit(func(o *flag.Flag) {
		if _, present := f.operations[o.Name]; present {
			auxiliary = true
		}
		switch o.Name {
		case "scope", "transaction", "target-sha256", "updater-pid", "request", "reason":
			auxiliary = true
		}
	})
	if count == 0 && !auxiliary {
		return false, nil
	}
	if count != 1 || set.NArg() != 0 {
		return true, errors.New("exactly one core handover operation is required")
	}
	allowed := map[string]bool{operation: true}
	if operation != "core-handover-protocol" {
		allowed["config"] = true
	}
	switch operation {
	case "core-handover-arm":
		for _, name := range []string{"scope", "transaction", "target-sha256", "updater-pid"} {
			allowed[name] = true
		}
	case "core-handover-status":
		allowed["request"] = true
	case "core-handover-revoke":
		allowed["request"] = true
		allowed["reason"] = true
	case "core-handover-revoke-all":
		allowed["reason"] = true
	}
	valid := operation == "core-handover-protocol" || configPath != ""
	set.Visit(func(o *flag.Flag) { valid = valid && allowed[o.Name] })
	if !valid {
		return true, fmt.Errorf("invalid flags for --%s", operation)
	}
	output, err := handovercli.Run(context.Background(), handovercli.Options{Operation: operation, ConfigPath: configPath, Scope: *f.scope, Transaction: *f.transaction, TargetSHA256: *f.target, UpdaterPID: *f.updater, RequestID: *f.request, Reason: *f.reason})
	if err != nil {
		return true, err
	}
	if output != "" {
		_, err = fmt.Fprintln(os.Stdout, output)
	}
	return true, err
}
