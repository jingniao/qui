package hostupdatestate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Event struct {
	Stage    string `json:"stage"`
	State    string `json:"state"`
	ExitCode *int   `json:"exitCode"`
	Error    string `json:"error"`
}

var ErrInvalidEvent = errors.New("invalid event arguments")

func eventLabel(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// External program output may contain arbitrary bytes. Keep every invalid byte
// visible, instead of silently replacing it with U+FFFD and losing the evidence.
func visibleUTF8(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && n == 1 {
			fmt.Fprintf(&b, `\x%02x`, s[0])
		} else {
			b.WriteString(s[:n])
		}
		s = s[n:]
	}
	return b.String()
}

func EncodeEvent(w io.Writer, event Event) error {
	if !eventLabel(event.Stage) || !eventLabel(event.State) || event.ExitCode != nil && (*event.ExitCode < 0 || *event.ExitCode > 255) {
		return ErrInvalidEvent
	}
	event.Error = visibleUTF8(event.Error)
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(event); err != nil {
		return err
	}
	n, err := w.Write(b.Bytes())
	if err == nil && n != b.Len() {
		err = io.ErrShortWrite
	}
	return err
}

func EncodeEventRawError(w io.Writer, stage, state string, exitCode *int, errorJSON []byte) error {
	errorJSON = bytes.TrimSpace(errorJSON)
	if len(errorJSON) == 0 || errorJSON[0] != '"' || !utf8.Valid(errorJSON) || !json.Valid(errorJSON) {
		return errors.New("error must be a valid JSON string literal")
	}
	var message string
	if err := json.Unmarshal(errorJSON, &message); err != nil {
		return err
	}
	// encoding/json accepts unpaired UTF-16 surrogates by replacing them. Reject
	// those spellings before decoding so structured failures never lose bytes.
	for i := 0; i < len(errorJSON); i++ {
		if errorJSON[i] != '\\' {
			continue
		}
		i++
		if errorJSON[i] != 'u' {
			continue
		}
		value, _ := strconv.ParseUint(string(errorJSON[i+1:i+5]), 16, 16)
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return errors.New("unpaired JSON surrogate")
		}
		if value < 0xd800 || value > 0xdbff {
			continue
		}
		if i+6 >= len(errorJSON) || errorJSON[i+1] != '\\' || errorJSON[i+2] != 'u' {
			return errors.New("unpaired JSON surrogate")
		}
		low, err := strconv.ParseUint(string(errorJSON[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return errors.New("unpaired JSON surrogate")
		}
		i += 6
	}
	return EncodeEvent(w, Event{Stage: stage, State: state, ExitCode: exitCode, Error: message})
}
