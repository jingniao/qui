package hostupdatestate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestEncodeEventBridgeLayout(t *testing.T) {
	for _, n := range []int{-1, 0, 143, 255} {
		var code *int
		if n >= 0 {
			code = &n
		}
		var b bytes.Buffer
		if err := EncodeEvent(&b, Event{Stage: "terminal", State: "failed", ExitCode: code}); err != nil {
			t.Fatal(err)
		}
		want := "null"
		if code != nil {
			j, _ := json.Marshal(n)
			want = string(j)
		}
		if b.String() != "{\"stage\":\"terminal\",\"state\":\"failed\",\"exitCode\":"+want+",\"error\":\"\"}\n" {
			t.Fatal(b.String())
		}
	}
}
func TestEncodeEventCharacters(t *testing.T) {
	value := "引号\"反斜杠\\\x00\b\f\n\r\t<>&\u2028\u2029"
	var b bytes.Buffer
	if err := EncodeEvent(&b, Event{Stage: "agent_install", State: "failed", Error: value}); err != nil {
		t.Fatal(err)
	}
	var got Event
	if err := json.Unmarshal(b.Bytes(), &got); err != nil || got.Error != value {
		t.Fatal(got, err)
	}
	if strings.Contains(b.String(), `\u003c`) {
		t.Fatal(b.String())
	}
	for _, stage := range []string{"bad\n", "中文", ""} {
		if err := EncodeEvent(io.Discard, Event{Stage: stage, State: "failed"}); err == nil {
			t.Fatal(stage)
		}
	}
}
func TestEncodeEventInvalidUTF8(t *testing.T) {
	var b bytes.Buffer
	if err := EncodeEvent(&b, Event{Stage: "terminal", State: "failed", Error: string([]byte{'a', 0xff, 0xc0, 'b'})}); err != nil {
		t.Fatal(err)
	}
	var got Event
	json.Unmarshal(b.Bytes(), &got)
	if got.Error != `a\xff\xc0b` {
		t.Fatal(got.Error)
	}
}
func TestEncodeEventRawError(t *testing.T) {
	for _, raw := range []string{`"中文\n<>&"`, `"\ud83d\ude00"`, `"\\ud800"`} {
		var b bytes.Buffer
		if err := EncodeEventRawError(&b, "agent_install", "failed", nil, []byte(raw)); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{`{}`, `null`, `"a" "b"`, `"\ud800"`, `"\udc00"`, `"\ud800x"`, string([]byte{'"', 0xff, '"'})} {
		var b bytes.Buffer
		if err := EncodeEventRawError(&b, "agent_install", "failed", nil, []byte(raw)); err == nil || b.Len() != 0 {
			t.Fatal("accepted", raw)
		}
	}
}

type brokenWriter struct{ short bool }

func (w brokenWriter) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, errors.New("broken output")
}
func TestEventOutputFailure(t *testing.T) {
	for _, short := range []bool{true, false} {
		if err := EncodeEvent(brokenWriter{short}, Event{Stage: "terminal", State: "success"}); err == nil {
			t.Fatal("false success")
		}
	}
}
