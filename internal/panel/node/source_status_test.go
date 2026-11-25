package node

import (
	"strings"
	"testing"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestValidateSourceAddressStatus(t *testing.T) {
	issue := func(kind, owner, address, iface string) *pb.SourceAddressIssue {
		return &pb.SourceAddressIssue{Kind: kind, OwnerId: owner, Address: address, Interface: iface}
	}
	many := make([]*pb.SourceAddressIssue, 1025)
	for i := range many {
		many[i] = issue("host_source", "", "192.0.2.1", "")
	}
	for _, tc := range []struct {
		name   string
		status *pb.SourceAddressStatus
		ok     bool
	}{
		{name: "old agent", status: nil, ok: true},
		{name: "nothing missing", status: &pb.SourceAddressStatus{Revision: 7}, ok: true},
		{name: "every kind", status: &pb.SourceAddressStatus{Revision: 7, Missing: []*pb.SourceAddressIssue{
			issue("host_source", "", "2001:db8::1", ""),
			issue("core_source", "", "", "wg0"),
			issue("core_source", "", "192.0.2.5", "eth0"),
			issue("inbound_egress", "inbound-1", "2001:db8::2", ""),
			issue("outbound_source", "outbound-1", "192.0.2.6", ""),
		}}, ok: true},
		{name: "check failed", status: &pb.SourceAddressStatus{Error: "source_check_failed"}, ok: true},
		{name: "negative revision", status: &pb.SourceAddressStatus{Revision: -1}},
		{name: "unknown error", status: &pb.SourceAddressStatus{Error: "boom"}},
		{name: "error with issues", status: &pb.SourceAddressStatus{Error: "source_check_failed", Missing: []*pb.SourceAddressIssue{issue("host_source", "", "192.0.2.1", "")}}},
		{name: "unknown kind", status: &pb.SourceAddressStatus{Missing: []*pb.SourceAddressIssue{issue("other", "", "192.0.2.1", "")}}},
		{name: "bad address", status: &pb.SourceAddressStatus{Missing: []*pb.SourceAddressIssue{issue("host_source", "", "not-an-ip", "")}}},
		{name: "host with owner", status: &pb.SourceAddressStatus{Missing: []*pb.SourceAddressIssue{issue("host_source", "x", "192.0.2.1", "")}}},
		{name: "host with interface", status: &pb.SourceAddressStatus{Missing: []*pb.SourceAddressIssue{issue("host_source", "", "192.0.2.1", "eth0")}}},
		{name: "inbound without owner", status: &pb.SourceAddressStatus{Missing: []*pb.SourceAddressIssue{issue("inbound_egress", "", "192.0.2.1", "")}}},
		{name: "core without binding", status: &pb.SourceAddressStatus{Missing: []*pb.SourceAddressIssue{issue("core_source", "", "", "")}}},
		{name: "outbound without owner", status: &pb.SourceAddressStatus{Missing: []*pb.SourceAddressIssue{issue("outbound_source", "", "192.0.2.1", "")}}},
		{name: "long interface", status: &pb.SourceAddressStatus{Missing: []*pb.SourceAddressIssue{issue("core_source", "", "", strings.Repeat("e", 65))}}},
		{name: "too many", status: &pb.SourceAddressStatus{Missing: many}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSourceAddressStatus(tc.status)
			if (err == nil) != tc.ok {
				t.Fatalf("validate = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}
