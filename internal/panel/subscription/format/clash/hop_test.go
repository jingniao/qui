package clash

import (
	"testing"

	"proxycontrol/internal/panel/subscription/proxy"
)

func hopEndpoint(hop string, interval int) proxy.Proxy {
	return proxy.Proxy{
		Name: "hy2", Server: "2001:db8::1", Port: 8443, Protocol: "hysteria2",
		Credential: proxy.Credential{Password: "secret"}, Security: "tls",
		TLS: &proxy.TLS{ServerName: "hy2.example.com"}, Transport: &proxy.Transport{Type: "quic"},
		QUIC: &proxy.QUIC{HopPorts: hop, HopInterval: interval},
	}
}

func TestHysteria2HopRangeInClash(t *testing.T) {
	value, err := convert(hopEndpoint("20000-20010", 30))
	if err != nil {
		t.Fatal(err)
	}
	if value.Ports != "20000-20010" || value.HopInterval != 30 {
		t.Fatalf("hop range was not exported: %+v", value)
	}
	plain, err := convert(hopEndpoint("", 0))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Ports != "" || plain.HopInterval != 0 {
		t.Fatalf("a plain endpoint gained hop fields: %+v", plain)
	}
}
