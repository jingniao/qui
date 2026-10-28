package xray

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/traffic"
	"strings"
	"testing"
	"time"
)

func TestDeviceGateOfficialCLIRecoveryVerificationTTLAndRestartRequest(t *testing.T) {
	for _, verified := range []bool{true, false} {
		t.Run(map[bool]string{true: "restored", false: "restart"}[verified], func(t *testing.T) {
			now := time.Unix(1000, 0)
			a := &Adapter{Directory: t.TempDir(), Now: func() time.Time { return now }, process: &exec.Cmd{}, statsEpoch: "epoch", current: core.RenderedConfig{StatsEndpoint: "127.0.0.1:12345", Data: []byte(`{"routing":{"domainStrategy":"AsIs","rules":[{"type":"field","ruleTag":"baseline","balancerTag":"pool"}],"balancers":[{"tag":"pool","selector":["direct"]}]},"outbounds":[{"tag":"block","protocol":"blackhole"}]}`), StatsAccounts: map[string]traffic.Counter{"user>>>email": {CredentialID: "c"}}}}
			calls := 0
			fail := false
			var payloads []string
			a.gateRunner = func(_ context.Context, args []string, out io.Writer) error {
				if args[0] != "api" || args[2] != "-s" || args[3] != "127.0.0.1:12345" {
					t.Fatal("nonofficial CLI shape")
				}
				if args[1] == "lsrules" {
					if verified {
						io.WriteString(out, `{"rules":[{"ruleTag":"baseline"}]}`)
					} else {
						io.WriteString(out, `{}`)
					}
					return nil
				}
				if args[1] != "adrules" || args[4] != "-append=false" || len(args) != 6 {
					t.Fatal("wrong reload command")
				}
				data, err := os.ReadFile(args[5])
				if err != nil {
					t.Fatal(err)
				}
				payloads = append(payloads, string(data))
				calls++
				if fail {
					fail = false
					return errors.New("private untrusted CLI error")
				}
				return nil
			}
			command := []byte(`{"ttl_seconds":180,"entries":[{"credential_id":"c","keys":["198.51.100.10"]}]}`)
			ctx := context.Background()
			if err := a.SetDeviceGate(ctx, command); err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Routing routingConfig `json:"routing"`
			}
			_ = json.Unmarshal([]byte(payloads[0]), &doc)
			if len(doc.Routing.Rules) != 2 || doc.Routing.Rules[0].RuleTag != "qui-gate-0" || doc.Routing.Rules[0].User[0] != "email" || len(doc.Routing.Balancers) != 1 {
				t.Fatal("gate did not preserve user/source scope or balancers")
			}
			if err := a.TickDeviceGates(ctx); err != nil || calls != 1 {
				t.Fatal("unchanged routing reloaded")
			}
			now = now.Add(180 * time.Second)
			if err := a.TickDeviceGates(ctx); err != nil || strings.Contains(payloads[len(payloads)-1], "qui-gate-") {
				t.Fatal("expired gate not removed")
			}
			fail = true
			err := a.SetDeviceGate(ctx, command)
			if err == nil || strings.Contains(err.Error(), "198.51") || strings.Contains(err.Error(), "private") || a.gateRecovery == verified || strings.Contains(payloads[len(payloads)-1], "qui-gate-") {
				t.Fatal("recovery or redaction failed")
			}
			if !verified {
				if _, err := a.Health(ctx); err == nil {
					t.Fatal("supervisor not asked to restore")
				}
			}
			files, _ := os.ReadDir(a.Directory)
			if len(files) != 0 {
				t.Fatal("gate payload retained on disk")
			}
		})
	}
}
