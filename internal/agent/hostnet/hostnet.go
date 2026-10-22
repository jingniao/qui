// Package hostnet implements the node side of the server-wide enforced source:
// the single nftables table that rewrites the source address of every new
// outbound connection leaving the interface that owns a configured address.
//
// The Agent never gains CAP_NET_ADMIN and never executes anything but this one
// fixed program. The packaged service runs
// `proxy-agent --apply-host-network <dir>`: on systemd the oneshot unit together
// with the path unit that triggers it, on OpenRC the proxy-agent-host-network
// watcher service (busybox inotifyd) that runs it once at start and again
// whenever desired.json changes. It reads one validated desired file from that
// directory, runs `nft -f -` with the rendered rules on stdin and writes one
// status file back. No shell is involved and the argument list of the one
// program is fixed.
package hostnet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
)

// Table is the one nftables table this feature owns. Nothing else on the host is
// read or changed, so deleting the table restores the previous behaviour.
const Table = "proxy_agent_source"

// The helper protocol is two files in one directory. Their names and the field
// set are fixed and minimal: an Agent that has updated itself writes
// DesiredFileName and waits for StatusFileName, while the unit that answers may
// still run the binary installed before that update. A new field may therefore
// only be added once every deployed helper can ignore it, which is why the
// desired file is decoded strictly.
const (
	DesiredFileName = "desired.json"
	StatusFileName  = "status.json"
)

// Desired is the helper input: the addresses the whole server must use and the
// address of the node's own core outbound (A), which must not be rewritten.
type Desired struct {
	ID         string `json:"id"`
	IPv4       string `json:"ipv4"`
	IPv6       string `json:"ipv6"`
	ExemptIPv4 string `json:"exempt_ipv4"`
	ExemptIPv6 string `json:"exempt_ipv6"`
}

// Status is the helper outcome for one Desired.ID.
type Status struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Validate re-checks the desired state with the shared contract validator. The
// helper is a separate process started by the packaged service (the systemd
// oneshot/path units or the OpenRC watcher) and does not trust its input.
func (d Desired) Validate() error {
	if d.ID == "" {
		return errors.New("missing id")
	}
	if err := (desiredstate.Network{HostSourceIPv4: d.IPv4, HostSourceIPv6: d.IPv6}).Validate(); err != nil {
		return err
	}
	if d.ExemptIPv4 != "" {
		if err := (desiredstate.Network{HostSourceIPv4: d.ExemptIPv4}).Validate(); err != nil {
			return fmt.Errorf("invalid exempt IPv4: %w", err)
		}
	}
	if d.ExemptIPv6 != "" {
		if err := (desiredstate.Network{HostSourceIPv6: d.ExemptIPv6}).Validate(); err != nil {
			return fmt.Errorf("invalid exempt IPv6: %w", err)
		}
	}
	return nil
}

// Clear reports whether no family is configured: the table is then removed
// instead of being rewritten, and an unconfigured family is never translated.
func (d Desired) Clear() bool { return d.IPv4 == "" && d.IPv6 == "" }

// InterfaceLookup resolves the interface that owns an address. An address that
// is not assigned locally is an error: the rule would never match.
type InterfaceLookup func(address string) (string, error)

// HostInterfaceLookup is the production lookup: it inspects the local
// interfaces.
func HostInterfaceLookup(address string) (string, error) {
	target := net.ParseIP(address)
	if target == nil {
		return "", fmt.Errorf("address %s does not parse", address)
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, entry := range interfaces {
		assigned, err := entry.Addrs()
		if err != nil {
			return "", err
		}
		for _, value := range assigned {
			ip, _, err := net.ParseCIDR(value.String())
			if err != nil {
				return "", err
			}
			if ip.Equal(target) {
				return entry.Name, nil
			}
		}
	}
	return "", fmt.Errorf("address %s is not assigned to any local interface", address)
}

// Render returns the rule file of one desired state. It creates the table first
// so the following delete also succeeds on a first install, and it keeps every
// rule in the one table this feature owns.
func Render(desired Desired, interfaces InterfaceLookup) ([]byte, error) {
	if err := desired.Validate(); err != nil {
		return nil, err
	}
	ipv4, ipv6 := canonicalAddress(desired.IPv4), canonicalAddress(desired.IPv6)
	exemptIPv4, exemptIPv6 := canonicalAddress(desired.ExemptIPv4), canonicalAddress(desired.ExemptIPv6)
	var rules strings.Builder
	rules.WriteString("table inet " + Table + " {} # makes the delete safe on first install\n")
	rules.WriteString("delete table inet " + Table + "\n")
	if ipv4 == "" && ipv6 == "" {
		return []byte(rules.String()), nil
	}
	if interfaces == nil {
		return nil, errors.New("no interface lookup")
	}
	rules.WriteString("table inet " + Table + " {\n")
	rules.WriteString("  chain postrouting {\n")
	rules.WriteString("    type nat hook postrouting priority 90; # before Docker/other srcnat chains (100); first NAT binding wins\n")
	if ipv4 != "" {
		name, err := interfaces(ipv4)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&rules, "    oifname %q ip saddr != { %s } snat ip to %s\n", name, sourceSet(ipv4, exemptIPv4), ipv4)
	}
	if ipv6 != "" {
		name, err := interfaces(ipv6)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&rules, "    oifname %q ip6 saddr != { %s } snat ip6 to %s\n", name, sourceSet(ipv6, exemptIPv6), ipv6)
	}
	rules.WriteString("  }\n")
	rules.WriteString("}\n")
	return []byte(rules.String()), nil
}

// canonicalAddress renders one configured address the way the core renderer does,
// so a mapped IPv4 address and its plain form produce the same rule and nft only
// ever sees an address of the family the rule belongs to. Validate has already
// accepted the value when this runs.
func canonicalAddress(value string) string {
	if value == "" {
		return ""
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return value
	}
	return address.Unmap().String()
}

// sourceSet is the set of sources a family keeps. The exemption is the node's
// core outbound address, so A wins for the traffic the core itself sends; the
// caller only sets it for its own family. An exemption equal to the source is
// omitted, so the rule never carries the same address twice.
func sourceSet(source, exempt string) string {
	if exempt == "" || exempt == source {
		return source
	}
	return source + ", " + exempt
}

// NftRun hands a rendered rule file to nft. It is injectable so the tests can
// assert the exact rules without an nftables host.
type NftRun func(ctx context.Context, rules []byte) error

// nftPaths are the fixed locations nft is looked up in, in order. The
// environment's PATH is never consulted: the helper executes exactly one known
// program and must not run whatever a modified environment points at.
var nftPaths = []string{"/usr/sbin/nft", "/sbin/nft", "/usr/bin/nft"}

// RunNft applies the rules with the first nft found in nftPaths, with a fixed
// argument list and the rules on stdin.
func RunNft(ctx context.Context, rules []byte) error {
	for _, path := range nftPaths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode()&0111 == 0 {
			continue
		}
		command := exec.CommandContext(ctx, path, "-f", "-")
		command.Stdin = bytes.NewReader(rules)
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("nft -f - failed: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	return fmt.Errorf("nft was not found in %s", strings.Join(nftPaths, ", "))
}

// Apply renders and applies one desired state. It is the helper's whole job.
func Apply(ctx context.Context, desired Desired, interfaces InterfaceLookup, run NftRun) error {
	rules, err := Render(desired, interfaces)
	if err != nil {
		return err
	}
	return run(ctx, rules)
}

// applyDesired is the render-and-apply step of the helper entry point. It is a
// variable so the tests can drive the file protocol without an nftables host.
var applyDesired = func(ctx context.Context, desired Desired) error {
	return Apply(ctx, desired, HostInterfaceLookup, RunNft)
}

// RunHelper is the --apply-host-network entry point. An absent desired file is a
// successful no-op, so the unit can start at boot before the first publish.
func RunHelper(ctx context.Context, directory string) error {
	desired, err := ReadDesired(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	applyErr := applyDesired(ctx, desired)
	status := Status{ID: desired.ID, OK: applyErr == nil}
	if applyErr != nil {
		status.Error = applyErr.Error()
	}
	if err := WriteStatus(directory, status); err != nil {
		return errors.Join(applyErr, err)
	}
	return applyErr
}

// ReadDesired reads and validates the helper input of one directory. Decoding is
// strict: an unknown field means the file was written by a newer Agent and
// applying it would silently drop a setting.
func ReadDesired(directory string) (Desired, error) {
	data, err := os.ReadFile(filepath.Join(directory, DesiredFileName))
	if err != nil {
		return Desired{}, err
	}
	var desired Desired
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&desired); err != nil {
		return Desired{}, fmt.Errorf("invalid %s: %w", DesiredFileName, err)
	}
	return desired, desired.Validate()
}

// WriteDesired publishes one desired state atomically, so the helper never reads
// a half-written file.
func WriteDesired(directory string, desired Desired) error {
	data, err := json.Marshal(desired)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(directory, DesiredFileName), append(data, '\n'))
}

// ReadStatus reads the last helper outcome. Unknown fields are tolerated: a
// newer helper must not break the Agent that is waiting for it.
func ReadStatus(directory string) (Status, error) {
	data, err := os.ReadFile(filepath.Join(directory, StatusFileName))
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return Status{}, fmt.Errorf("invalid %s: %w", StatusFileName, err)
	}
	return status, nil
}

// WriteStatus publishes one outcome atomically. The temporary file is hidden, so
// it is neither watched by the systemd path unit nor mistaken for a result.
func WriteStatus(directory string, status Status) error {
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(directory, StatusFileName), append(data, '\n'))
}

// writeAtomic replaces path in one rename after flushing the new content.
func writeAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return vault.SyncDir(filepath.Dir(path))
}
