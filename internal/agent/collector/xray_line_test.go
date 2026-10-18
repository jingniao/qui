package collector

import "testing"

func TestXrayAccessLineAllowlistedMetadataOnly(t *testing.T) {
	event, err := ParseXrayAccessLine("2026/10/02 10:00:00.123 from 192.0.2.5:5000 accepted tcp:example.com:443 [in ==> out] email: credential:credential-id")
	if err != nil || event.Domain != "example.com" || event.Port != 443 || event.AccountID != "credential-id" || event.InboundID != "in" || event.OutboundTag != "out" {
		t.Fatal(event, err)
	}
	event, err = ParseXrayAccessLine("2026/10/02 from 192.0.2.5:5000 accepted udp:[2001:db8::1]:53 [in -> block] free form rejection reason")
	if err != nil || event.TargetIP != "2001:db8::1" || event.Network != "udp" {
		t.Fatal(event, err)
	}
	for _, line := range []string{"accepted tcp:https://example.com/private:443 [in -> direct]", "accepted tcp:example.com/private:443 [in -> direct]", "accepted tcp:example.com:443 [in -> direct] email: username@private.example", "accepted tcp:example.com:443 [in -> direct]\nCookie: secret", "arbitrary process error and password"} {
		if _, err := ParseXrayAccessLine(line); err == nil {
			t.Fatal("untrusted content accepted as metadata", line)
		}
	}
}

func TestHTTPAccessURLIsReducedToAuthorityOnly(t *testing.T) {
	event, err := ParseXrayAccessLine("from 192.0.2.1:123 accepted http://example.test:8080/PRIVATE-PATH?token=PRIVATE-QUERY [http-in -> direct]")
	if err != nil || event.Domain != "example.test" || event.Port != 8080 || event.Network != "tcp" {
		t.Fatal(event, err)
	}
	event, err = ParseXrayAccessLine("from 192.0.2.1:123 accepted example.test:443 [http-in -> direct]")
	if err != nil || event.Domain != "example.test" || event.Port != 443 {
		t.Fatal(event, err)
	}
	event, err = ParseXrayAccessLine("from 192.0.2.1:123 accepted //example.test:443 [http-in -> direct]")
	if err != nil || event.Domain != "example.test" || event.Port != 443 {
		t.Fatal(event, err)
	}
	if _, err := ParseXrayAccessLine("from 192.0.2.1:123 accepted http://password:secret@example.test/private [http-in -> direct]"); err == nil {
		t.Fatal("URL credentials accepted")
	}
}
