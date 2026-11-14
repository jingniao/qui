package backup

import (
	"context"
	"testing"
)

func TestValidateOutboundSource(t *testing.T) {
	source, _, _ := sourceFixture(t)
	ctx := context.Background()
	for _, v := range []struct {
		protocol, ip, iface string
		valid               bool
	}{{"direct", "192.0.2.8", "eth0", true}, {"direct", "2001:db8::1", "", true}, {"direct", "bad", "", false}, {"direct", "", "bad/name", false}, {"block", "192.0.2.8", "", false}, {"block", "", "eth0", false}, {"block", "", "", true}} {
		if _, err := source.Database.Exec("DELETE FROM outbounds"); err != nil {
			t.Fatal(err)
		}
		if _, err := source.Database.Exec("INSERT INTO outbounds(id,name,node_id,protocol,enabled,encrypted_settings,support_tcp,support_udp,failure_policy,created_at,source_ip,source_interface) SELECT 'source','source',id,?,0,x'00',1,0,'Block','now',?,? FROM nodes LIMIT 1", v.protocol, v.ip, v.iface); err != nil {
			t.Fatal(err)
		}
		if err := validateOutboundSource(ctx, source.Database); (err == nil) != v.valid {
			t.Fatalf("%+v: %v", v, err)
		}
	}
	rewindOutboundSource(t, source.Database)
	if err := validateOutboundSource(ctx, source.Database); err != nil {
		t.Fatal(err)
	}
}
