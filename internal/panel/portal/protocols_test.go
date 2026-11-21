package portal

import (
	"encoding/json"
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
	for _, status := range []string{"online", "degraded", "core_error", "error", "reconnecting", "offline", "pending", "maintenance", "disabled", "deleted"} {
		online := status == "online" || status == "degraded" || status == "core_error"
		if got := displayOnline(status); got != online {
			t.Fatalf("%s: online = %v, want %v", status, got, online)
		}
		raw, err := json.Marshal(Node{Status: status, Online: displayOnline(status), Protocols: sortedProtocols(nil)})
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["status"] != status || fields["online"] != online {
			t.Fatalf("%s: portal status contract = %s", status, raw)
		}
	}
}
