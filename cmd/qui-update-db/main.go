package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"proxycontrol/internal/platform/hostupdatedb"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
func run(ctx context.Context, args []string, out, diagnostic io.Writer) int {
	if len(args) == 1 {
		switch args[0] {
		case "--protocol":
			fmt.Fprintln(out, "qui-update-database-v1")
			return 0
		case "--version":
			fmt.Fprintln(out, "qui-update-db", version)
			return 0
		}
	}
	if len(args) == 0 {
		fmt.Fprintln(diagnostic, "database operation is required")
		return 70
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	registry := flags.String("registry", "", "root-private registry")
	account := flags.String("account", "", "panel account")
	source := flags.String("source", "", "offline source")
	destination := flags.String("destination", "", "destination")
	backup := flags.String("backup", "", "backup")
	check := flags.String("check-directory", "", "root-private check directory")
	checker := flags.String("checker", "", "target panel executable")
	restore := flags.Bool("restore", false, "remove old WAL and SHM")
	staged := flags.Bool("owner-staged", false, "read Owner-authorized private staged input")
	failure := 70
	if args[0] == "copy" {
		failure = 1
	}
	if args[0] == "compare" {
		failure = 2
	}
	if err := flags.Parse(args[1:]); err != nil {
		return failure
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(diagnostic, "unexpected positional arguments")
		return failure
	}
	allowed := map[string]map[string]bool{
		"copy":      {"registry": true, "account": true, "source": true, "destination": true, "restore": true, "owner-staged": true},
		"compare":   {"account": true, "source": true, "backup": true},
		"preflight": {"check-directory": true, "backup": true, "checker": true},
	}
	valid := allowed[args[0]] != nil
	flags.Visit(func(f *flag.Flag) {
		if !allowed[args[0]][f.Name] {
			valid = false
		}
	})
	switch args[0] {
	case "copy":
		valid = valid && *registry != "" && *account != "" && *source != "" && *destination != ""
	case "compare":
		valid = valid && *account != "" && *source != "" && *backup != ""
	case "preflight":
		valid = valid && *check != "" && *backup != "" && *checker != ""
	}
	if !valid {
		fmt.Fprintln(diagnostic, "invalid database operation arguments")
		return failure
	}
	var err error
	switch args[0] {
	case "copy":
		err = hostupdatedb.Copy(ctx, hostupdatedb.CopyOptions{Registry: *registry, Account: *account, Source: *source, Destination: *destination, Restore: *restore, OwnerStaged: *staged})
	case "compare":
		var equal bool
		equal, err = hostupdatedb.Compare(ctx, *account, *source, *backup)
		if err == nil && !equal {
			return 1
		}
	case "preflight":
		var result hostupdatedb.CheckResult
		result, err = hostupdatedb.Preflight(ctx, hostupdatedb.PreflightOptions{CheckDirectory: *check, Backup: *backup, Checker: *checker})
		if err == nil {
			if _, err = fmt.Fprintf(out, "checker-exit: %d\n", result.ExitCode); err == nil {
				_, err = out.Write(result.Output)
			}
		} else {
			_, writeErr := diagnostic.Write(result.Output)
			if writeErr != nil {
				fmt.Fprintln(diagnostic, writeErr)
			}
		}
	}
	if err != nil {
		fmt.Fprintln(diagnostic, err)
		return failure
	}
	return 0
}
