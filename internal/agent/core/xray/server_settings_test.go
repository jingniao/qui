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
	if len(report) != 3 {
		t.Fatalf("expected exactly three native-supported combinations, got %d", len(report))
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
