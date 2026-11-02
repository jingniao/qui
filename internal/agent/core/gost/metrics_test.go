package gost

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// gostMetricsFixture is a trimmed answer of the pinned release: the measured
// counter lines of G0, one series of another service, other metric families, a
// comment line and a timestamp. Values are floats, so the exporter prints a large
// counter in scientific notation.
const gostMetricsFixture = `# HELP gost_service_transfer_input_bytes_total
# TYPE gost_service_transfer_input_bytes_total counter
gost_service_transfer_input_bytes_total{client="127.0.0.1",host="192",service="tcp-forward-tcp"} 86
gost_service_transfer_input_bytes_total{client="127.0.0.1",host="192",service="both-forward-udp"} 12
gost_service_transfer_input_bytes_total{client="127.0.0.1",host="192",service="not-our-service-tcp"} 4096
gost_service_transfer_output_bytes_total{client="127.0.0.1",host="192",service="tcp-forward-tcp"} 2.097357e+06
gost_service_transfer_output_bytes_total{client="127.0.0.1",host="192",service="both-forward-udp"} 18 1735689600000
gost_service_requests_total{client="127.0.0.1",host="192",service="tcp-forward-tcp"} 2
gost_services 1
`

// TestParseMetricsSumsForwardServices covers what the node counter has to read:
// both directions, scientific notation, datagram-sized series, a timestamp and the
// series of a service this renderer did not create.
func TestParseMetricsSumsForwardServices(t *testing.T) {
	services := map[string]bool{"tcp-forward-tcp": true, "both-forward-udp": true}
	upload, download, err := parseMetrics(strings.NewReader(gostMetricsFixture), services)
	if err != nil {
		t.Fatal(err)
	}
	if upload != 98 || download != 2097375 {
		t.Fatalf("upload=%d download=%d", upload, download)
	}
}

// TestParseMetricsMatchesAnUnescapedLabelValue: a service name containing a quote
// is printed escaped and has to be unescaped before it is compared.
func TestParseMetricsMatchesAnUnescapedLabelValue(t *testing.T) {
	line := `gost_service_transfer_input_bytes_total{service="odd\"name",client="1.2.3.4"} 7` + "\n"
	upload, _, err := parseMetrics(strings.NewReader(line), map[string]bool{"odd\"name": true})
	if err != nil || upload != 7 {
		t.Fatalf("upload=%d err=%v", upload, err)
	}
}

// TestParseMetricsCountsEveryExpandedServiceOnce derives the counter filter from a
// rendered range and proves each expanded service is summed exactly once. A range
// becomes several services, and the node counter must not lose or double any of
// them; a series this node did not render is ignored.
func TestParseMetricsCountsEveryExpandedServiceOnce(t *testing.T) {
	rendered, err := (&Adapter{Directory: goldenDirectory}).Render(context.Background(), poolState())
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if err := json.Unmarshal(rendered.Data, &config); err != nil {
		t.Fatal(err)
	}
	services := serviceNames(config)
	if len(services) != 4 {
		t.Fatalf("rendered services: %v", services)
	}
	lines := ""
	for name := range services {
		lines += `gost_service_transfer_input_bytes_total{client="127.0.0.1",host="192",service="` + name + `"} 10` + "\n"
		lines += `gost_service_transfer_output_bytes_total{client="127.0.0.1",host="192",service="` + name + `"} 20` + "\n"
	}
	lines += `gost_service_transfer_input_bytes_total{client="127.0.0.1",host="192",service="fwd-pool-forward-20402-tcp"} 4096` + "\n"
	upload, download, err := parseMetrics(strings.NewReader(lines), services)
	if err != nil {
		t.Fatal(err)
	}
	if upload != 40 || download != 80 {
		t.Fatalf("upload=%d download=%d", upload, download)
	}
}

// TestParseMetricsRejectsMalformedInput keeps a truncated or hostile answer from
// being read as a smaller total.
func TestParseMetricsRejectsMalformedInput(t *testing.T) {
	services := map[string]bool{"service": true}
	for _, tc := range []struct{ name, input string }{
		{"no value", `gost_service_transfer_input_bytes_total{service="service"}` + "\n"},
		{"unterminated labels", `gost_service_transfer_input_bytes_total{service="service"` + "\n"},
		{"unterminated value", `gost_service_transfer_input_bytes_total{service="service} 1` + "\n"},
		{"unquoted value", `gost_service_transfer_input_bytes_total{service=service} 1` + "\n"},
		{"not a number", `gost_service_transfer_input_bytes_total{service="service"} many` + "\n"},
		{"negative", `gost_service_transfer_input_bytes_total{service="service"} -1` + "\n"},
		{"not finite", `gost_service_transfer_input_bytes_total{service="service"} NaN` + "\n"},
		{"trailing data", `gost_service_transfer_input_bytes_total{service="service"} 1 2 3` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := parseMetrics(strings.NewReader(tc.input), services); err == nil {
				t.Fatal("malformed metrics answer accepted")
			}
		})
	}
}

// TestParseCountAcceptsExporterSyntax pins the two written forms of a byte count.
func TestParseCountAcceptsExporterSyntax(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  uint64
	}{
		{"0", 0},
		{"86", 86},
		{"2.097357e+06", 2097357},
		{"4.194714e+06", 4194714},
		{"1.000000e+00", 1},
	} {
		got, err := parseCount(tc.value)
		if err != nil || got != tc.want {
			t.Fatalf("%s: %d %v", tc.value, got, err)
		}
	}
	for _, bad := range []string{"", "abc", "-1", "Inf", "-Inf", "NaN", "1e30"} {
		if _, err := parseCount(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}
