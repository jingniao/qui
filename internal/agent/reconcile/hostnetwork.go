package reconcile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"runtime"
	"time"

	"proxycontrol/internal/agent/hostnet"
	"proxycontrol/pkg/contracts/desiredstate"
)

// hostNetworkFailedCode is the temporary apply failure code of a node whose
// server-wide enforced source could not be applied or could not prove that the
// panel is still reachable. It is never cached: the helper can be installed or
// started later, so the same revision is retried.
const hostNetworkFailedCode = "host_network_failed"

const (
	// hostNetworkTimeout bounds the whole helper interaction. It is deliberately
	// independent of the apply context: the helper must reach the state of the
	// revision that is about to be committed even when the apply that triggered
	// it ran out of time.
	hostNetworkTimeout = 15 * time.Second
	// hostNetworkDialTimeout bounds one reachability dial.
	hostNetworkDialTimeout = 5 * time.Second
	// hostNetworkPollInterval is how often the helper status file is re-read.
	hostNetworkPollInterval = 100 * time.Millisecond
)

// hostNetworkStatusWait bounds how long the Agent waits for the helper to report
// the outcome of one desired file. It is a variable so the tests do not sleep for
// the production ten seconds.
var hostNetworkStatusWait = 10 * time.Second

// hostNetworkSupported reports whether this host can enforce the source. It is a
// variable so the tests can drive the Linux path on a development host that has
// no nftables.
var hostNetworkSupported = func() bool { return runtime.GOOS == "linux" }

// hostNetworkDial proves that a new connection of one family still reaches the
// panel. It is a variable so the tests can fail or stall the proof.
var hostNetworkDial = func(ctx context.Context, network, address string) error {
	dialCtx, cancel := context.WithTimeout(ctx, hostNetworkDialTimeout)
	defer cancel()
	var dialer net.Dialer
	connection, err := dialer.DialContext(dialCtx, network, address)
	if err != nil {
		return err
	}
	return connection.Close()
}

// applyHostNetwork mirrors one revision in the helper. A nil network is the empty
// network: the revision configures nothing, so the table is removed.
func (r *Reconciler) applyHostNetwork(ctx context.Context, network *desiredstate.Network) error {
	value := desiredstate.Network{}
	if network != nil {
		value = *network
	}
	sync := r.HostNetworkSync
	if sync == nil {
		sync = r.syncHostNetwork
	}
	return sync(ctx, value)
}

// syncHostNetwork writes the helper's desired file and waits for its answer, so
// the rules on the host always mirror the revision the Agent runs. The
// reachability proof afterwards is what makes the change safe: a rule that breaks
// new outbound connections rolls the revision back instead of taking the node off
// the control plane.
func (r *Reconciler) syncHostNetwork(ctx context.Context, network desiredstate.Network) error {
	configured := network.HostSourceIPv4 != "" || network.HostSourceIPv6 != ""
	directory := r.HostNetworkDirectory
	if directory == "" {
		if !configured {
			return nil
		}
		return errors.New("the host-network helper is not installed")
	}
	if _, err := os.Stat(directory); err != nil {
		if !configured && errors.Is(err, fs.ErrNotExist) {
			// An install that predates the feature has neither the helper nor
			// anything to clear, so it keeps working unchanged.
			return nil
		}
		return fmt.Errorf("the host-network helper directory %s is unavailable: %w", directory, err)
	}
	if !hostNetworkSupported() {
		if !configured {
			return nil
		}
		return errors.New("the server-wide enforced source requires Linux")
	}
	if !configured {
		// Nothing is configured, so the helper is only needed when the
		// retained desired file still enforces a family. Writing a clear
		// request to a helper that is missing or broken would fail every
		// apply on a node that never used the feature.
		retained, err := hostnet.ReadDesired(directory)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err == nil && retained.Clear():
			return nil
		}
	}
	// A fresh context: the helper must still be driven to the state of the
	// revision that is about to be committed when the apply context is done.
	proofCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hostNetworkTimeout)
	defer cancel()
	id, err := hostNetworkID()
	if err != nil {
		return err
	}
	desired := hostnet.Desired{
		ID:         id,
		IPv4:       network.HostSourceIPv4,
		IPv6:       network.HostSourceIPv6,
		ExemptIPv4: exemptAddress(network.OutboundSourceIP, 4),
		ExemptIPv6: exemptAddress(network.OutboundSourceIP, 6),
	}
	if err := hostnet.WriteDesired(directory, desired); err != nil {
		return err
	}
	if err := r.waitHostNetworkStatus(proofCtx, directory, id); err != nil {
		return err
	}
	if !configured {
		return nil
	}
	if r.ControlAddress == "" {
		return errors.New("the panel control address is unknown, so the enforced source cannot be proven")
	}
	for _, family := range hostNetworkFamilies(network) {
		available, err := controlFamilyAvailable(proofCtx, r.ControlAddress, family.family)
		if err != nil {
			return err
		}
		if !available {
			continue
		}
		if err := hostNetworkDial(proofCtx, family.network, r.ControlAddress); err != nil {
			return fmt.Errorf("new %s connections no longer reach the panel at %s: %w", family.network, r.ControlAddress, err)
		}
	}
	return nil
}

// hostNetworkFamily is one configured address family and the dial network that
// proves it.
type hostNetworkFamily struct {
	network string
	family  int
}

// hostNetworkFamilies is the set of families the revision configures. An
// unconfigured family is not translated and is not proven.
func hostNetworkFamilies(network desiredstate.Network) []hostNetworkFamily {
	families := []hostNetworkFamily{}
	if network.HostSourceIPv4 != "" {
		families = append(families, hostNetworkFamily{network: "tcp4", family: 4})
	}
	if network.HostSourceIPv6 != "" {
		families = append(families, hostNetworkFamily{network: "tcp6", family: 6})
	}
	return families
}

// exemptAddress is the node's core outbound address when it belongs to the family
// the rule is for: the core's own traffic must keep its own source, and an
// address of the other family cannot appear in an ip or ip6 rule.
func exemptAddress(source string, family int) string {
	ip := net.ParseIP(source)
	if ip == nil {
		return ""
	}
	if family == 4 && ip.To4() == nil {
		return ""
	}
	if family == 6 && ip.To4() != nil {
		return ""
	}
	return source
}

// controlFamilyAvailable reports whether the panel control address has an address
// of one family. A literal of the other family, or a name without a record of
// that family, is not an error: it means there is nothing to prove for that
// family, and a node may legitimately enforce IPv6 while its panel is only
// reachable over IPv4.
func controlFamilyAvailable(ctx context.Context, address string, family int) (bool, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false, fmt.Errorf("invalid panel control address %s: %w", address, err)
	}
	if literal := net.ParseIP(host); literal != nil {
		if family == 4 {
			return literal.To4() != nil, nil
		}
		return literal.To4() == nil, nil
	}
	network := "ip4"
	if family == 6 {
		network = "ip6"
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, network, host)
	if err != nil {
		return false, fmt.Errorf("cannot resolve the panel control address %s: %w", host, err)
	}
	return len(addresses) > 0, nil
}

// waitHostNetworkStatus waits for the helper to report the outcome of the desired
// file it was just handed. A status of another id is a stale report from an
// earlier publish and is ignored.
func (r *Reconciler) waitHostNetworkStatus(ctx context.Context, directory, id string) error {
	deadline := time.Now().Add(hostNetworkStatusWait)
	for {
		status, err := hostnet.ReadStatus(directory)
		switch {
		case err == nil && status.ID == id && status.OK:
			return nil
		case err == nil && status.ID == id:
			return fmt.Errorf("the host-network helper failed: %s", status.Error)
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("cannot read the host-network helper status: %w", err)
		}
		if err := hostNetworkPoll(ctx, deadline); err != nil {
			return fmt.Errorf("the host-network helper did not report within %s", hostNetworkStatusWait)
		}
	}
}

// hostNetworkPoll sleeps one interval, never past the wait deadline.
func hostNetworkPoll(ctx context.Context, deadline time.Time) error {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	if remaining > hostNetworkPollInterval {
		remaining = hostNetworkPollInterval
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// hostNetworkID is a fresh identifier per desired file, so a status file left
// behind by an earlier publish can never be mistaken for the answer to this one.
func hostNetworkID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
