// Package hostupdatestate implements the published host updater's journal v1.
package hostupdatestate

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const magic = "proxycontrol-update-journal-v1"

var ErrInvalidJournal = errors.New("invalid journal fields")

type Journal struct {
	Component, Previous, Target                            string
	WasActive                                              bool
	Backup, DatabaseSource, Phase, CoreRequest, CoreHelper string
}

func validJournal(j Journal) error {
	if j.Component != "panel" && j.Component != "agent" {
		return fmt.Errorf("%w: component", ErrInvalidJournal)
	}
	switch j.Phase {
	case "prepared", "started", "committed", "restored":
	default:
		return fmt.Errorf("%w: phase", ErrInvalidJournal)
	}
	return nil
}

func parseJournal(data []byte) (Journal, error) {
	var j Journal
	lines := bytes.Split(data, []byte{'\n'})
	if string(lines[0]) != magic {
		return j, errors.New("invalid journal magic")
	}
	values := map[string]string{}
	for _, line := range lines[1:] {
		key, value, ok := strings.Cut(string(line), "=")
		if !ok {
			continue
		}
		if _, seen := values[key]; !seen {
			values[key] = value
		}
	}
	j = Journal{Component: values["component"], Previous: "未知", Target: "未知", Backup: values["backup"], DatabaseSource: values["database_source"], Phase: values["phase"], CoreRequest: values["core_request"], CoreHelper: values["core_helper"]}
	if v, ok := values["previous"]; ok {
		j.Previous = v
	}
	if v, ok := values["target"]; ok {
		j.Target = v
	}
	switch values["was_active"] {
	case "true":
		j.WasActive = true
	case "false":
	default:
		return j, errors.New("invalid journal was_active")
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return j, errors.New("journal contains NUL")
	}
	return j, validJournal(j)
}

func journalBytes(j Journal) ([]byte, error) {
	if err := validJournal(j); err != nil {
		return nil, err
	}
	for _, v := range []string{j.Previous, j.Target, j.Backup, j.DatabaseSource, j.CoreRequest, j.CoreHelper} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return nil, fmt.Errorf("%w: field contains LF, CR or NUL", ErrInvalidJournal)
		}
	}
	if j.Component == "panel" && (j.CoreRequest != "" || j.CoreHelper != "") {
		return nil, fmt.Errorf("%w: panel cannot contain core fields", ErrInvalidJournal)
	}
	if j.CoreHelper != "" && j.CoreHelper != "v1" {
		return nil, fmt.Errorf("%w: core helper version", ErrInvalidJournal)
	}
	text := fmt.Sprintf("%s\ncomponent=%s\nprevious=%s\ntarget=%s\nwas_active=%s\nbackup=%s\ndatabase_source=%s\nphase=%s\n", magic, j.Component, j.Previous, j.Target, strconv.FormatBool(j.WasActive), j.Backup, j.DatabaseSource, j.Phase)
	if j.Component == "agent" {
		text += fmt.Sprintf("core_request=%s\ncore_helper=%s\n", j.CoreRequest, j.CoreHelper)
	}
	return []byte(text), nil
}
