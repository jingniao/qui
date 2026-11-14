package node

import (
	"errors"
	"fmt"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

var ErrOutboundSourceUnsupported = errors.New("outbound_source_unsupported")
var ErrHostSourceExemptUnsupported = errors.New("host_source_exempt_unsupported")

// CapabilityError is a refusal the API answers with its own code and a Chinese
// message; errors.Is matches the sentinel it wraps.
type CapabilityError struct {
	Err     error
	Message string
}

func (e *CapabilityError) Error() string { return e.Message }
func (e *CapabilityError) Unwrap() error { return e.Err }

// Code is the API error code, the sentinel's text.
func (e *CapabilityError) Code() string { return e.Err.Error() }

// RequireOutboundSourceSupport refuses per-outbound source bindings an enabled
// Xray/sing-box core cannot render (GOST renders no outbound), and extra host
// exemptions the server's host-network helper cannot apply.
func RequireOutboundSourceSupport(state desiredstate.State, c *pb.Capabilities, name string) error {
	if name == "" {
		name = state.NodeID
	}
	for _, out := range state.Outbounds {
		if !out.Enabled || (out.SourceIP == "" && out.SourceInterface == "") {
			continue
		}
		for _, core := range EnabledCores(c) {
			if (core.Name == desiredstate.CoreXray || core.Name == desiredstate.CoreSingBox) && !core.OutboundSourceSupported {
				return &CapabilityError{Err: ErrOutboundSourceUnsupported, Message: fmt.Sprintf("服务器「%s」的 Agent 不支持按出站指定源 IP，请先升级 Agent", name)}
			}
		}
	}
	v4, v6 := desiredstate.ExtraHostExemptions(state)
	if len(v4)+len(v6) > 0 && !c.GetHostSourceExtraExemptSupported() {
		return &CapabilityError{Err: ErrHostSourceExemptUnsupported, Message: fmt.Sprintf("服务器「%s」的主机网络 helper 不支持出站源 IP 与强制出站同时使用，请在该服务器上运行一次 qui-update --component agent", name)}
	}
	return nil
}
