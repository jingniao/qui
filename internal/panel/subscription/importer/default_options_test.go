package importer

import "testing"

func TestURIDefaultOptions(t *testing.T) {
	for _, option := range []string{"headerType=none", "allowInsecure=0", "insecure=0", "allow_insecure=0", "ed=2048"} {
		t.Run(option, func(t *testing.T) {
			_, code, _ := uriProxy("trojan://secret@example.com:443?" + option)
			if code != "" {
				t.Fatalf("unexpected rejection: %s", code)
			}
		})
	}
}

func TestURIRejectedOptions(t *testing.T) {
	for option, want := range map[string]string{"insecure=1": "insecure_tls", "plugin=test": "unsupported_transport", "unknown=test": "unsupported_transport", "headerType=http": "unsupported_transport"} {
		t.Run(option, func(t *testing.T) {
			_, code, _ := uriProxy("trojan://secret@example.com:443?" + option)
			if code != want {
				t.Fatalf("got %s, want %s", code, want)
			}
		})
	}
}
