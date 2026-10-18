package agentprocess

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestBoundedStartupFrameRejectsAmbiguityAndPreservesLifetimeBytes(t *testing.T) {
	r := Request{Kind: "agent-start-v1", Nonce: strings.Repeat("a", 64), NodeID: "node", Revision: 7, Version: "test"}
	data, _ := json.Marshal(r)
	frame := append(append([]byte{}, data...), '\n')
	parsed, channel, err := ReadRequest(bytes.NewReader(append(frame, 'x')))
	if err != nil || parsed != r {
		t.Fatal(parsed, err)
	}
	left, err := io.ReadAll(channel)
	if err != nil || string(left) != "x" {
		t.Fatal("buffered lifetime input disappeared", string(left), err)
	}
	for _, bad := range [][]byte{
		data,
		append(bytes.Repeat([]byte{' '}, MaxHandshakeBytes), frame...),
		bytes.Replace(frame, []byte(`"revision":7`), []byte(`"revision":7,"revision":8`), 1),
		bytes.Replace(frame, []byte(`"revision":7`), []byte(`"revision":-1`), 1),
		bytes.Replace(frame, []byte(`"revision":7`), []byte(`"revision":7,"command":"run"`), 1),
		append(append([]byte{}, data...), []byte("{}\n")...),
	} {
		if _, _, err := ReadRequest(bytes.NewReader(bad)); err == nil {
			t.Fatal("invalid startup frame accepted")
		}
	}
	for _, wrong := range []struct {
		node     string
		revision int64
		version  string
	}{{"other", 7, "test"}, {"node", 8, "test"}, {"node", 7, "other"}} {
		var output bytes.Buffer
		if Confirm(r, wrong.node, wrong.revision, wrong.version, &output) == nil || output.Len() != 0 {
			t.Fatal("wrong runtime confirmed")
		}
	}
}

func TestDegradedConfirmationCannotBeUsedForCandidateReadiness(t *testing.T) {
	r := Request{Kind: "agent-start-v1", Nonce: strings.Repeat("b", 64), NodeID: "node", Revision: 1, Version: "test"}
	var output bytes.Buffer
	if ConfirmHealth(r, "node", 1, "test", false, &output) == nil || output.Len() != 0 {
		t.Fatal("candidate confirmed degraded startup")
	}
	r.AllowDegraded = true
	if err := ConfirmHealth(r, "node", 1, "test", false, &output); err != nil {
		t.Fatal(err)
	}
	var reply Ready
	if err := json.Unmarshal(output.Bytes(), &reply); err != nil || reply.Kind != "agent-degraded-v1" {
		t.Fatal("degraded control reported healthy", err)
	}
}
