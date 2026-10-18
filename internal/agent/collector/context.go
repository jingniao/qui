package collector

import (
	"errors"
	"proxycontrol/pkg/contracts/access"
	"proxycontrol/pkg/contracts/desiredstate"
	"time"
)

type Account struct {
	ID, UserID, DeviceID string
	Internal             bool
}
type Inbound struct {
	Protocol string
	Accounts map[string]Account
	Internal bool
}
type Exit struct {
	ID, Protocol, RuleID, ChainID, AccountID string
	NativeTag                                string
	LogDomain                                bool
}

// EgressGroupProtocol marks a compiled exit whose concrete outbound the core
// chooses itself (an upstream Xray balancer or sing-box urltest group). The
// marker never reaches an access record: Record resolves the member the core's
// own outbound log line named through the rule's Selections entry.
const EgressGroupProtocol = "_egress_group_"

// Context intentionally contains no passwords, private keys, certificates,
// endpoints or payload. It belongs to exactly one compiled process revision.
type Context struct {
	NodeID     string
	Revision   int64
	Inbounds   map[string]Inbound
	Exits      map[string]Exit
	Selections map[string]map[string]Exit
}

func NewContext(state desiredstate.State) *Context {
	context := &Context{NodeID: state.NodeID, Revision: state.Revision, Inbounds: map[string]Inbound{}, Exits: map[string]Exit{}}
	credentials := map[string]desiredstate.Credential{}
	for _, credential := range state.Credentials {
		credentials[credential.ID] = credential
	}
	for _, inbound := range state.Inbounds {
		if !inbound.Enabled {
			continue
		}
		entry := Inbound{Protocol: inbound.Protocol, Accounts: map[string]Account{}, Internal: len(inbound.CredentialIDs) > 0}
		for _, id := range inbound.CredentialIDs {
			credential, ok := credentials[id]
			if !ok {
				continue
			}
			account := Account{ID: id, UserID: credential.UserID, DeviceID: credential.DeviceID, Internal: credential.Internal}
			entry.Accounts[id] = account
			if credential.Username != "" {
				entry.Accounts[credential.Username] = account
			}
			if !credential.Internal {
				entry.Internal = false
			}
		}
		context.Inbounds[inbound.ID] = entry
	}
	return context
}

func (c *Context) Record(event XrayEvent, at time.Time) (access.Rollup, bool, error) {
	var record access.Rollup
	inbound, exists := c.Inbounds[event.InboundID]
	if !exists || inbound.Internal {
		return record, false, nil
	}
	exit, exists := c.Exits[event.OutboundTag]
	if event.SelectedOutbound != "" {
		exit, exists = c.Selections[event.OutboundTag][event.SelectedOutbound]
	}
	if !exists {
		return record, false, errors.New("unknown compiled access exit")
	}
	if !exit.LogDomain {
		return record, false, nil
	}
	record = access.Rollup{Bucket: at.UTC(), NodeID: c.NodeID, Revision: c.Revision, InboundProtocol: inbound.Protocol, Domain: event.Domain, TargetIP: event.TargetIP, TargetPort: event.Port, Network: event.Network, RuleID: exit.RuleID, OutboundID: exit.ID, OutboundProtocol: exit.Protocol, ChainID: exit.ChainID, Connections: 1, Action: "Proxy", Allowed: true}
	accountID := event.AccountID
	if accountID == "" {
		accountID = exit.AccountID
	}
	if accountID != "" {
		account, known := inbound.Accounts[accountID]
		if !known {
			return access.Rollup{}, false, errors.New("unknown access account")
		}
		if account.Internal {
			return access.Rollup{}, false, nil
		}
		record.UserID, record.DeviceID = account.UserID, account.DeviceID
	}
	if exit.Protocol == "direct" {
		record.Action = "Direct"
		record.ChainID = ""
		record.EgressKnown = true
		record.FinalEgressNodeID = c.NodeID
	}
	if exit.Protocol == "block" || event.Rejected {
		record.Action = "Block"
		record.Allowed = false
		record.EgressKnown = false
		record.FinalEgressNodeID = ""
		record.ChainID = ""
	}
	if err := record.Normalize(); err != nil {
		return access.Rollup{}, false, err
	}
	return record, true, nil
}
