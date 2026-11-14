package geoip

import (
	"bytes"
	"compress/gzip"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedTableLoads(t *testing.T) {
	if err := Err(); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}$`).MatchString(Version()) {
		t.Fatalf("version = %q", Version())
	}
	table, _ := current()
	if len(table.v4) < 100000 || len(table.v6) < 100000 || len(table.codes) < 200 {
		t.Fatalf("table too small: %d IPv4, %d IPv6, %d codes", len(table.v4), len(table.v6), len(table.codes))
	}
}

// The expected codes are long-standing allocations in the DB-IP data (netcup,
// APNIC/China Telecom, Google), chosen to stay stable across table refreshes.
func TestCountry(t *testing.T) {
	for address, want := range map[string]string{
		"5.45.96.43":        "DE",
		"1.0.1.5":           "CN",
		"8.8.8.8":           "US",
		"::ffff:5.45.96.43": "DE",
		"2a03:4000::1":      "DE",
		"":                  "",
		"not-an-ip":         "",
		"qui.lluu.de":       "",
		"10.0.0.1":          "",
		"192.168.1.1":       "",
		"100.64.0.1":        "",
		"127.0.0.1":         "",
		"0.0.0.0":           "",
		"255.255.255.255":   "",
		"224.0.0.1":         "",
		"169.254.1.1":       "",
		"::1":               "",
		"fe80::1":           "",
		"fe80::1%eth0":      "",
		"fd00::1":           "",
		"5.45.96.43:443":    "",
		" 5.45.96.43":       "",
	} {
		if got := Country(address); got != want {
			t.Errorf("Country(%q) = %q, want %q", address, got, want)
		}
	}
}

func TestDecodeRejectsDamagedTables(t *testing.T) {
	good := gunzip(t, packed)
	cases := map[string][]byte{
		"not gzip":    []byte("plain"),
		"bad magic":   gz(t, append([]byte("QGEO9\n"), good[len(magic):]...)),
		"truncated":   gz(t, good[:len(good)/2]),
		"trailing":    gz(t, append(append([]byte{}, good...), 0)),
		"header only": gz(t, []byte(magic)),
	}
	for name, data := range cases {
		if _, err := decode(data); err == nil || !strings.HasPrefix(err.Error(), "geoip:") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := decode(packed); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkCountry(b *testing.B) {
	_ = Err()
	for i := 0; i < b.N; i++ {
		Country("5.45.96.43")
		Country("2a03:4000::1")
	}
}

func gunzip(t *testing.T, data []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := out.ReadFrom(reader); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func gz(t *testing.T, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
