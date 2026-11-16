package xray

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestNativeProbeDoesNotAdvertiseFailedCombinations(t *testing.T) {
	seen := map[string]bool{}
	report := probeServerSettings(context.Background(), "26.3.27", func(ctx context.Context, path string) error {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc configDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		// Outbound probes carry no inbound; their stream decides the same way.
		if len(doc.Inbounds) == 0 {
			out := doc.Outbounds[0]
			if out.Stream.Security == "tls" || out.Stream.Network == "grpc" {
				return errors.New("unsupported native cell")
			}
			return nil
		}
		in := doc.Inbounds[0]
		if in.Stream.Security == "tls" || in.Stream.Network == "grpc" {
			return errors.New("unsupported native cell")
		}
		mode := ""
		if in.Stream.XHTTP != nil {
			mode = in.Stream.XHTTP.Mode
		}
		seen[in.Stream.Security+"/"+in.Stream.Network+"/"+mode] = true
		return nil
	})
	inbound, outbound := 0, 0
	for _, c := range report {
		if c.Direction == "outbound" {
			outbound++
		} else {
			inbound++
		}
	}
	// Outbound: vless none/ws, reality tcp, reality tcp vision and reality xhttp.
	if inbound != 3 || outbound != 4 {
		t.Fatalf("expected three inbound and four outbound native-supported combinations, got %d and %d", inbound, outbound)
	}
	for _, c := range report {
		if c.Security == "tls" || c.Transport == "grpc" {
			t.Fatal("unprobed settings were advertised")
		}
	}
	for _, mode := range []string{"auto", "packet-up", "stream-up", "stream-one"} {
		if !seen["reality/xhttp/"+mode] {
			t.Fatal("unprobed xhttp mode", mode)
		}
	}
	called := false
	if got := probeServerSettings(context.Background(), "1.8.16", func(context.Context, string) error { called = true; return nil }); len(got) != 0 || called {
		t.Fatal("unverified old version gained capability")
	}
}
