package proxy

import (
	"strings"
	"testing"
)

func TestOmissionWarningsGroupAndLimitNames(t *testing.T) {
	var warnings OmissionWarnings
	for i := 0; i < 12; i++ {
		warnings.Add("A", "xhttp_mode")
	}
	got := warnings.Warnings("clash")
	if len(got) != 1 || !strings.Contains(got[0].Message, "等 12 个：xhttp 模式不是 auto") || strings.Count(got[0].Message, "「A」") != 10 {
		t.Fatalf("%+v", got)
	}
}

func TestClientFormatOmissionReasons(t *testing.T) {
	cases := []struct {
		p    Proxy
		code string
	}{
		{Proxy{Security: "reality"}, "reality_no_vision"},
		{Proxy{Security: "reality", Transport: &Transport{Type: "grpc"}}, "reality_grpc"},
		{Proxy{TLS: &TLS{ALPN: []string{"http/1.1"}}, Transport: &Transport{Type: "xhttp", XHTTP: &XHTTPOptions{Mode: "auto"}}}, "xhttp_alpn"},
		{Proxy{Transport: &Transport{Type: "xhttp", XHTTP: &XHTTPOptions{Mode: "stream-one"}}}, "xhttp_mode"},
	}
	for _, tc := range cases {
		if got := ClientFormatOmission(tc.p, "sing-box"); got != tc.code {
			t.Errorf("got %s want %s", got, tc.code)
		}
	}
}
