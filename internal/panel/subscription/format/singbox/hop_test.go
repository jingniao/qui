package singbox

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

func TestHysteria2HopRangeInSingbox(t *testing.T) {
	value, err := renderOutbound(hopEndpoint("20000-20010", 30))
	if err != nil {
		t.Fatal(err)
	}
	if len(value.ServerPorts) != 1 || value.ServerPorts[0] != "20000:20010" || value.HopInterval != "30s" {
		t.Fatalf("hop range was not exported: %+v", value)
	}
	plain, err := renderOutbound(hopEndpoint("", 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.ServerPorts) != 0 || plain.HopInterval != "" {
		t.Fatalf("a plain endpoint gained hop fields: %+v", plain)
	}
}
