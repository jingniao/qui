package uri

import (
	"strings"
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

func TestHysteria2HopRangeInURI(t *testing.T) {
	link, err := Link(hopEndpoint("20000-20010", 30))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(link, "mport=20000-20010") {
		t.Fatalf("URI did not carry the hop range: %s", link)
	}
	plain, err := Link(hopEndpoint("", 0))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "mport=") {
		t.Fatalf("a plain endpoint gained mport: %s", plain)
	}
}
