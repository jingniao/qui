package proxy

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func TestShadowsocksMeasuredCipherIdentityMatrix(t *testing.T) {
	for _, tc := range []struct {
		method                    string
		size                      int
		identity, mihomo, singbox bool
	}{
		{"2022-blake3-aes-256-gcm", 32, false, true, true},
		{"2022-blake3-aes-256-gcm", 32, true, true, true},
		{"2022-blake3-aes-128-gcm", 16, false, true, true},
		{"2022-blake3-aes-128-gcm", 16, true, false, true},
		{"2022-blake3-chacha20-poly1305", 32, false, true, false},
	} {
		password := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{251}, tc.size))
		if tc.identity {
			password += ":" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{255}, tc.size))
		}
		c, err := ValidateShadowsocks(tc.method, password)
		if err != nil || c.Identity != tc.identity {
			t.Fatalf("%s identity=%t: %v", tc.method, tc.identity, err)
		}
		for format, supported := range map[string]bool{"uri": true, "base64": true, "clash": tc.mihomo, "sing-box": tc.singbox, "clash-legacy": false} {
			if (c.Omission(format) == "") != supported {
				t.Fatalf("%s identity=%t format=%s", tc.method, tc.identity, format)
			}
		}
	}
}

func TestSS2022EveryKeyIsCanonicalAndSized(t *testing.T) {
	for _, size := range []int{16, 32} {
		method := "2022-blake3-aes-256-gcm"
		if size == 16 {
			method = "2022-blake3-aes-128-gcm"
		}
		good := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{251}, size))
		for _, bad := range []string{good[:len(good)-1], good + "\n", good[:4] + "\r\n" + good[4:], " " + good, good + " ", base64.URLEncoding.EncodeToString(bytes.Repeat([]byte{255}, size))} {
			for _, password := range []string{bad, good + ":" + bad, bad + ":" + good} {
				if _, err := ValidateShadowsocks(method, password); !errors.Is(err, ErrSSKeyEncoding) {
					t.Fatalf("accepted noncanonical %d-byte key: %v", size, err)
				}
			}
		}
		for _, wrongSize := range []int{0, 1, 15, 17, 31, 33, 64} {
			if _, err := ValidateShadowsocks(method, base64.StdEncoding.EncodeToString(make([]byte, wrongSize))); !errors.Is(err, ErrSSKeyLength) {
				t.Fatalf("accepted %d-byte key for %s: %v", wrongSize, method, err)
			}
		}
	}
}
