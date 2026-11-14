package importer

import (
	"encoding/json"
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"strings"
	"testing"
)

func TestQUICImportExports(t *testing.T) {
	for _, tc := range []struct{ input, congestion, relay, alpn string }{
		{"tuic://6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef:secret@example.com:443?congestion_control=bbr&udp_relay_mode=native&alpn=h3", "bbr", "native", "h3"},
		{"tuic://6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef:secret@example.com:443?congestion_control=cubic&udp_relay_mode=quic&alpn=h3,accept-alpn", "cubic", "quic", "h3,accept-alpn"},
		{"proxies:\n- {name: tuic, type: tuic, server: example.com, port: 443, uuid: 6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef, password: secret, congestion-controller: new_reno, udp-relay-mode: quic, alpn: [h3, accept-alpn]}", "new_reno", "quic", "h3,accept-alpn"},
		{"proxies:\n- {name: tuic, type: tuic, server: example.com, port: 443, uuid: 6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef, password: secret, congestion-controller: cubic}", "cubic", "", ""},
		{"proxies:\n- {name: tuic, type: tuic, server: example.com, port: 443, uuid: 6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef, password: secret, udp-relay-mode: quic}", "", "quic", ""},
		{"tuic://6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef:secret@example.com:443", "", "", ""},
		{"proxies:\n- {name: hy, type: hysteria2, server: example.com, port: 443, password: secret, ports: 20000-30000, hop-interval: 30, up: '100 Mbps', down: 200}", "", "", ""},
	} {
		_, entries := Analyze([]byte(tc.input))
		if len(entries) != 1 {
			t.Error(tc.input)
			continue
		}
		if entries[0].Protocol == "tuic" {
			if strings.Join(entries[0].TLS.ALPN, ",") != tc.alpn {
				t.Errorf("import ALPN = %v, want %s", entries[0].TLS.ALPN, tc.alpn)
			}
			if q := entries[0].QUIC; q != nil {
				if q.CongestionControl != tc.congestion || q.UDPRelayMode != tc.relay {
					t.Errorf("import QUIC = %+v, want %s/%s", q, tc.congestion, tc.relay)
				}
			} else if tc.congestion != "" || tc.relay != "" {
				t.Error("missing imported QUIC settings")
			}
		}
		profile := format.Profile{Proxies: entries}
		for _, exporter := range []format.Exporter{clash.Meta{}, singbox.Exporter{}, uri.URI{}, uri.Base64{}} {
			if entries[0].Protocol == "hysteria2" && (exporter.ID() == "uri" || exporter.ID() == "base64") {
				continue
			}
			out, err := exporter.Render(profile)
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Warnings) != 0 {
				t.Errorf("%s warnings: %+v", exporter.ID(), out.Warnings)
			}
			wants := []string{}
			if entries[0].Protocol == "hysteria2" {
				wants = []string{"server_ports", "20000:30000", "hop_interval", "30s"}
				if exporter.ID() == (clash.Meta{}).ID() {
					wants = []string{"ports: 20000-30000", "hop-interval: 30", "up: 100", "down: 200"}
				}
			}
			for _, want := range wants {
				if !strings.Contains(string(out.Body), want) {
					t.Fatalf("missing %s: %s", want, out.Body)
				}
			}
			if entries[0].Protocol == "tuic" {
				wantALPN := tc.alpn
				if wantALPN == "" {
					wantALPN = "h3"
				}
				if exporter.ID() == "sing-box" {
					var document struct {
						Outbounds []struct {
							Type       string
							Congestion string `json:"congestion_control"`
							Relay      string `json:"udp_relay_mode"`
							TLS        struct{ ALPN []string }
						}
					}
					if err := json.Unmarshal(out.Body, &document); err != nil {
						t.Fatal(err)
					}
					found := false
					for _, outbound := range document.Outbounds {
						if outbound.Type != "tuic" {
							continue
						}
						found = true
						if strings.Join(outbound.TLS.ALPN, ",") != wantALPN || outbound.Congestion != tc.congestion || outbound.Relay != tc.relay {
							t.Errorf("sing-box TUIC parameters lost: %+v", outbound)
						}
					}
					if !found {
						t.Error("missing sing-box TUIC outbound")
					}
				} else {
					result, returned := Analyze(out.Body)
					if result.Outcome != "ok" || len(returned) != 1 {
						t.Errorf("%s reimport: %+v", exporter.ID(), result)
						continue
					}
					if strings.Join(returned[0].TLS.ALPN, ",") != wantALPN {
						t.Errorf("%s reimport ALPN = %v, want %s", exporter.ID(), returned[0].TLS.ALPN, wantALPN)
					}
					if q := returned[0].QUIC; q != nil {
						if q.CongestionControl != tc.congestion || q.UDPRelayMode != tc.relay {
							t.Errorf("%s reimport QUIC = %+v, want %s/%s", exporter.ID(), q, tc.congestion, tc.relay)
						}
					} else if tc.congestion != "" || tc.relay != "" {
						t.Errorf("%s lost QUIC settings", exporter.ID())
					}
				}
			}
		}
		link, err := uri.Link(entries[0])
		if err != nil {
			t.Fatal(err)
		}
		want := "congestion_control=" + tc.congestion
		if entries[0].Protocol == "hysteria2" {
			want = "mport=20000-30000"
		} else if tc.congestion == "" {
			want = "alpn=h3"
		}
		if !strings.Contains(link, want) {
			t.Fatal(link)
		}
	}
}

func TestQUICURIImport(t *testing.T) {
	for _, input := range []string{"tuic://6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef:secret@example.com:443?congestion_control=bbr&udp_relay_mode=native&alpn=h3", "hysteria2://secret@example.com:443?mport=20000-30000"} {
		p, code, _ := uriProxy(input)
		if code != "" || p.QUIC == nil {
			t.Fatalf("import: %s %+v", code, p)
		}
		if p.Protocol == "tuic" && (p.QUIC.CongestionControl != "bbr" || p.QUIC.UDPRelayMode != "native" || p.TLS.ALPN[0] != "h3") {
			t.Fatal(p)
		}
		if p.Protocol == "hysteria2" && p.QUIC.HopPorts != "20000-30000" {
			t.Fatal(p)
		}
	}
}

func TestQUICInvalidURIImport(t *testing.T) {
	for _, input := range []string{"tuic://6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef:secret@example.com:443?congestion_control=invalid", "tuic://6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef:secret@example.com:443?udp_relay_mode=invalid", "hysteria2://secret@example.com:443?mport=20000,30000"} {
		if _, code, _ := uriProxy(input); code == "" {
			t.Fatal(input)
		}
	}
	for _, option := range []string{"congestion-controller: invalid", "udp-relay-mode: invalid", "unknown-option: value"} {
		result, entries := Analyze([]byte("proxies:\n- {name: tuic, type: tuic, server: example.com, port: 443, uuid: 6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef, password: secret, " + option + "}"))
		if len(entries) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "unsupported_option" {
			t.Errorf("%s accepted or wrong reason: %+v", option, result)
		}
	}
	for _, option := range []string{"congestion-controller: cubic", "udp-relay-mode: quic"} {
		result, entries := Analyze([]byte("proxies:\n- {name: hy, type: hysteria2, server: example.com, port: 443, password: secret, " + option + "}"))
		if len(entries) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "invalid_protocol_combination" {
			t.Errorf("%s accepted for hysteria2 or wrong reason: %+v", option, result)
		}
	}
}

func TestHysteria2YAMLImport(t *testing.T) {
	for _, up := range []string{"100", "'100 Mbps'", "'100Mbps'", "'100 Kbps'", "1.5"} {
		_, entries := Analyze([]byte("proxies:\n- name: hy\n  type: hysteria2\n  server: example.com\n  port: 443\n  password: secret\n  ports: 20000-30000\n  hop-interval: 30\n  up: " + up + "\n  down: 200\n"))
		if up == "'100 Kbps'" || up == "1.5" {
			if len(entries) != 0 {
				t.Fatal(up)
			}
			continue
		}
		if len(entries) != 1 {
			t.Fatal(up)
		}
		q := entries[0].QUIC
		if q.HopPorts != "20000-30000" || q.HopInterval != 30 || q.UpMbps != 100 || q.DownMbps != 200 {
			t.Fatal(q)
		}
	}
}
