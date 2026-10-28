package xray

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"proxycontrol/pkg/contracts/devicegate"
	"strconv"
	"strings"
	"time"
)

func (a *Adapter) gateNow() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}
func (a *Adapter) SetDeviceGate(ctx context.Context, data []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	err := a.deviceGate.Set(data, a.gateNow())
	if err != nil {
		a.deviceGate.Clear()
	}
	applyErr := a.tickDeviceGateLocked(ctx)
	if err != nil {
		return err
	}
	return applyErr
}
func (a *Adapter) TickDeviceGates(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tickDeviceGateLocked(ctx)
}
func gateRouting(data []byte, entries []devicegate.Entry, names map[string][]string) ([]byte, []string, error) {
	var doc struct {
		Routing   routingConfig    `json:"routing"`
		Outbounds []outboundConfig `json:"outbounds"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil, nil, errors.New("device gate baseline unavailable")
	}
	block := ""
	for _, o := range doc.Outbounds {
		if o.Protocol == "blackhole" {
			block = o.Tag
			break
		}
	}
	if block == "" {
		return nil, nil, errors.New("device gate block outbound unavailable")
	}
	tags := []string{}
	for _, r := range doc.Routing.Rules {
		if r.RuleTag == "" || strings.HasPrefix(r.RuleTag, "qui-gate-") {
			return nil, nil, errors.New("device gate baseline tags invalid")
		}
		tags = append(tags, r.RuleTag)
	}
	rules := []route{}
	for _, e := range entries {
		if len(names[e.CredentialID]) == 0 || len(e.Keys) == 0 {
			continue
		}
		rules = append(rules, route{Type: "field", RuleTag: "qui-gate-" + strconv.Itoa(len(rules)), User: names[e.CredentialID], Source: e.Keys, OutboundTag: block})
	}
	doc.Routing.Rules = append(rules, doc.Routing.Rules...)
	payload, err := json.Marshal(struct {
		Routing routingConfig `json:"routing"`
	}{doc.Routing})
	return payload, tags, err
}

// gateCLI never returns stdout, stderr, command arguments or source material in
// an error. Only the installed official CLI is used to load routing JSON.
func (a *Adapter) gateCLI(ctx context.Context, payload []byte) error {
	f, err := os.CreateTemp(a.Directory, ".gate-*.json")
	if err != nil {
		return errors.New("device gate file unavailable")
	}
	defer os.Remove(f.Name())
	if f.Chmod(0600) != nil {
		f.Close()
		return errors.New("device gate permissions unavailable")
	}
	_, err = f.Write(payload)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("device gate write failed")
	}
	timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if a.runGateCommand(timeout, []string{"api", "adrules", "-s", a.current.StatsEndpoint, "-append=false", f.Name()}, io.Discard) != nil {
		return errors.New("device gate routing reload failed")
	}
	return nil
}
func (a *Adapter) runGateCommand(ctx context.Context, args []string, out io.Writer) error {
	if a.gateRunner != nil {
		return a.gateRunner(ctx, args, out)
	}
	c := exec.CommandContext(ctx, a.Path, args...)
	c.Stdout = out
	c.Stderr = io.Discard
	return c.Run()
}
func (a *Adapter) verifyGateFree(ctx context.Context, tags []string) bool {
	return a.verifyRouting(ctx, tags, true)
}
func (a *Adapter) verifyRouting(ctx context.Context, tags []string, gateFree bool) bool {
	timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// CLI output is bounded and retained only for this verification.
	var out limitedGateOutput
	if a.runGateCommand(timeout, []string{"api", "lsrules", "-s", a.current.StatsEndpoint}, &out) != nil || out.overflow {
		return false
	}
	var listed any
	if json.Unmarshal(out.data, &listed) != nil {
		return false
	}
	found := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, value := range x {
				if k == "ruleTag" {
					if s, ok := value.(string); ok {
						found[s] = true
					}
				}
				walk(value)
			}
		case []any:
			for _, value := range x {
				walk(value)
			}
		}
	}
	walk(listed)
	for tag := range found {
		if gateFree && strings.HasPrefix(tag, "qui-gate-") {
			return false
		}
	}
	for _, tag := range tags {
		if !found[tag] {
			return false
		}
	}
	return len(tags) > 0
}

type limitedGateOutput struct {
	data     []byte
	overflow bool
}

func (b *limitedGateOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.data)+n > 1<<20 {
		b.overflow = true
	} else {
		b.data = append(b.data, p...)
	}
	return n, nil
}

func (a *Adapter) probeDeviceGate(ctx context.Context) bool {
	if len(a.current.Data) == 0 {
		return !a.gateFailed
	}
	var doc struct {
		Routing routingConfig `json:"routing"`
	}
	if json.Unmarshal(a.current.Data, &doc) != nil {
		return false
	}
	tags := []string{}
	for _, rule := range doc.Routing.Rules {
		tags = append(tags, rule.RuleTag)
	}
	return !a.gateRecovery && a.verifyRouting(ctx, tags, false)
}
func (a *Adapter) tickDeviceGateLocked(ctx context.Context) error {
	if a.process == nil || a.current.StatsEndpoint == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(a.current.StatsEndpoint)
	if err != nil || host != "127.0.0.1" {
		return errors.New("device gate requires loopback")
	}
	entries := a.deviceGate.Entries(a.gateNow())
	// A fresh process starts gate-free. Avoid any reload until a command exists.
	if len(entries) == 0 && a.gateDigest == "" {
		return nil
	}
	names := map[string][]string{}
	for name, c := range a.current.StatsAccounts {
		if strings.HasPrefix(name, "user>>>") {
			names[c.CredentialID] = append(names[c.CredentialID], strings.TrimPrefix(name, "user>>>"))
		}
	}
	payload, tags, err := gateRouting(a.current.Data, entries, names)
	if err != nil {
		a.deviceGate.Clear()
		return err
	}
	hash := sha256.Sum256(append([]byte(a.statsEpoch), payload...))
	digest := hex.EncodeToString(hash[:])
	if digest == a.gateDigest {
		return nil
	}
	if a.gateCLI(ctx, payload) == nil {
		a.gateDigest = digest
		a.gateFailed = false
		return nil
	}
	a.deviceGate.Clear()
	a.gateFailed = true
	baseline, _, restoreErr := gateRouting(a.current.Data, nil, names)
	// Use an independent context: a cancelled control stream must not prevent
	// recovery of a non-atomic routing reload.
	recovery, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if restoreErr != nil || a.gateCLI(recovery, baseline) != nil || !a.verifyGateFree(recovery, tags) {
		a.gateRecovery = true
	} else {
		a.gateFailed = false
	}

	a.gateDigest = ""
	return errors.New("device gate failed open; routing recovery requested")
}
