package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"proxycontrol/internal/agent/statereset"
)

type stateResetFlags struct{ operation, target *string }

func registerStateResetFlags(set *flag.FlagSet) *stateResetFlags {
	return &stateResetFlags{set.String("reset-legacy-state", "", "fixed root legacy reset operation"), set.String("reset-target-sha256", "", "installed new Agent SHA256 for reset")}
}
func executeStateReset(set *flag.FlagSet, f *stateResetFlags, path, version string) (bool, error) {
	if *f.operation == "" && *f.target == "" {
		return false, nil
	}
	valid := *f.operation != "" && path != "" && set.NArg() == 0
	set.Visit(func(o *flag.Flag) {
		valid = valid && (o.Name == "config" || o.Name == "reset-legacy-state" || o.Name == "reset-target-sha256")
	})
	if !valid {
		return true, errors.New("legacy state reset requires only config, operation and target digest")
	}
	state, err := statereset.Run(context.Background(), *f.operation, path, *f.target, version)
	if err == nil {
		fmt.Println(state)
	}
	return true, err
}
