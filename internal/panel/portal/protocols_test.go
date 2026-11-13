package portal

import (
	"reflect"
	"testing"

	"proxycontrol/internal/panel/subscription/proxy"
)

func TestDirectProtocolsSortedDedupedAndDirectOnly(t *testing.T) {
	node := func(id string) proxy.Origin { return proxy.Origin{Node: proxy.Ref{ID: id}} }
	got := directProtocols([]proxy.Proxy{
		{Origin: node("a"), Protocol: "vless"},
		{Origin: node("a"), Protocol: "trojan"},
		{Origin: node("a"), Protocol: "vless"},
		{Origin: node("b"), Protocol: "socks5"},
		{Origin: proxy.Origin{Relayed: true, Node: proxy.Ref{ID: "a"}}, Protocol: "shadowsocks"},
		{Origin: node("a"), Protocol: "hysteria2", ClientChain: "x"},
	})
	if want := []string{"trojan", "vless"}; !reflect.DeepEqual(sortedProtocols(got["a"]), want) {
		t.Fatalf("a: %v", got["a"])
	}
	if !reflect.DeepEqual(sortedProtocols(got["b"]), []string{"socks5"}) {
		t.Fatalf("b: %v", got["b"])
	}
	if sortedProtocols(got["c"]) == nil || len(sortedProtocols(nil)) != 0 {
		t.Fatal("empty list must be a non-nil empty slice")
	}
}
