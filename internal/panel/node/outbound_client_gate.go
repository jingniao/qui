package node

import (
	"errors"
	"fmt"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

var ErrOutboundClientUnsupported = errors.New("outbound_client_unsupported")

// RequireOutboundClientSupport refuses outbound client settings (v1.6.0: ALPN,
// uTLS fingerprint, WebSocket host, Reality client, xhttp) unless every enabled
// Xray/sing-box core reported that exact outbound combination. An older Agent
// would refuse the unknown fields when it decodes the revision, so disabled
// outbounds count too: they travel in the same desired state.
func RequireOutboundClientSupport(state desiredstate.State, c *pb.Capabilities, name string) error {
	if name == "" {
		name = state.NodeID
	}
	for _, out := range state.Outbounds {
		combination, required := desiredstate.OutboundSettingsCombination(out.Protocol, out.Settings)
		if !required {
			continue
		}
		for _, core := range EnabledCores(c) {
			if core.Name != desiredstate.CoreXray && core.Name != desiredstate.CoreSingBox {
				continue
			}
			if desiredstate.RequireOutboundSettings(core.Name, SettingsContractCapability(core), out.Protocol, out.Settings) != nil {
				return &CapabilityError{Err: ErrOutboundClientUnsupported, Message: fmt.Sprintf("服务器「%s」的 %s 核心不支持出站 %s 使用的连接参数（%s），请先升级该服务器的 Agent", name, core.Name, out.ID, combination.String())}
			}
		}
	}
	return nil
}
