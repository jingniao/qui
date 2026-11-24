package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"proxycontrol/internal/platform/hostupdatestate"
)

var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
func run(args []string, input io.Reader, out, diagnostic io.Writer) int {
	fail := func(code int, err error) int { fmt.Fprintln(diagnostic, err); return code }
	if len(args) == 1 && (args[0] == "--protocol" || args[0] == "--version") {
		value := "qui-update-state-v1"
		if args[0] == "--version" {
			value = "qui-update-state " + version
		}
		if _, err := fmt.Fprintln(out, value); err != nil {
			return fail(70, err)
		}
		return 0
	}
	if len(args) == 0 {
		return fail(70, fmt.Errorf("state operation is required"))
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	allowed := map[string][]string{
		"journal-write": {"snapshot", "component", "previous", "target", "was-active", "backup", "database-source", "phase", "core-request", "core-helper"},
		"journal-read":  {"file", "backups"},
		"discard":       {"registry", "snapshot", "discarded"},
		"event":         {"stage", "state", "exit-code"},
	}
	names, ok := allowed[args[0]]
	if !ok {
		return fail(70, fmt.Errorf("unknown state operation: %s", args[0]))
	}
	values := map[string]*string{}
	for _, name := range names {
		values[name] = flags.String(name, "", name)
	}
	var rawError bool
	if args[0] == "event" {
		flags.BoolVar(&rawError, "error-json", false, "stdin contains a JSON string literal")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return 70
	}
	if flags.NArg() != 0 {
		return fail(70, fmt.Errorf("unexpected positional arguments"))
	}
	get := func(name string) string { return *values[name] }
	var err error
	switch args[0] {
	case "event":
		var code *int
		if get("exit-code") != "null" {
			n, parseErr := strconv.Atoi(get("exit-code"))
			if parseErr != nil || n < 0 || n > 255 {
				return fail(70, fmt.Errorf("exit-code must be null or 0..255"))
			}
			code = &n
		}
		body, readErr := io.ReadAll(input)
		if readErr != nil {
			return fail(1, readErr)
		}
		if rawError {
			err = hostupdatestate.EncodeEventRawError(out, get("stage"), get("state"), code, body)
		} else {
			err = hostupdatestate.EncodeEvent(out, hostupdatestate.Event{Stage: get("stage"), State: get("state"), ExitCode: code, Error: string(body)})
		}
	case "discard":
		if get("registry") == "" || (get("snapshot") == "") == (get("discarded") == "") {
			return fail(70, fmt.Errorf("registry and exactly one of snapshot or discarded are required"))
		}
		if get("snapshot") != "" {
			err = hostupdatestate.DiscardSnapshot(get("registry"), get("snapshot"))
		} else {
			err = hostupdatestate.RemoveDiscarded(get("registry"), get("discarded"))
		}
	case "journal-write":
		if get("snapshot") == "" || get("component") == "" || get("phase") == "" || (get("was-active") != "true" && get("was-active") != "false") {
			return fail(70, fmt.Errorf("invalid journal-write arguments"))
		}
		badCore := false
		flags.Visit(func(f *flag.Flag) {
			if get("component") == "panel" && (f.Name == "core-request" || f.Name == "core-helper") {
				badCore = true
			}
		})
		if badCore {
			return fail(70, fmt.Errorf("panel cannot accept core arguments"))
		}
		err = hostupdatestate.WriteJournal(get("snapshot"), hostupdatestate.Journal{Component: get("component"), Previous: get("previous"), Target: get("target"), WasActive: get("was-active") == "true", Backup: get("backup"), DatabaseSource: get("database-source"), Phase: get("phase"), CoreRequest: get("core-request"), CoreHelper: get("core-helper")})
	case "journal-read":
		if get("file") == "" || get("backups") == "" {
			return fail(70, fmt.Errorf("file and backups are required"))
		}
		var j hostupdatestate.Journal
		j, err = hostupdatestate.ReadJournal(get("file"), get("backups"))
		if err == nil {
			for _, v := range []string{j.Component, j.Previous, j.Target, strconv.FormatBool(j.WasActive), j.Backup, j.DatabaseSource, j.Phase, j.CoreRequest, j.CoreHelper} {
				if _, err = fmt.Fprint(out, v, "\x00"); err != nil {
					break
				}
			}
		}
	}
	if errors.Is(err, hostupdatestate.ErrInvalidEvent) || args[0] == "journal-write" && errors.Is(err, hostupdatestate.ErrInvalidJournal) {
		return fail(70, err)
	}
	if err != nil {
		return fail(1, err)
	}
	return 0
}
