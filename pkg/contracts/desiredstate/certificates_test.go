package desiredstate

import (
	"strings"
	"testing"
)

// certificateFiles is the accepted shape: one absolute, already clean pair under
// the node's own certificate directory.
func certificateFiles() ProtocolSettings {
	return ProtocolSettings{Security: "tls", CertificatePath: CertificateDirectory + "/a.crt", PrivateKeyPath: CertificateDirectory + "/a.key"}
}

// A node-side file pair is one more TLS material set, so it obeys the same path
// rules the Agent applies when it reads the files: absolute, already clean, and
// inside the one directory the panel may point at.
func TestCertificateFilePathsAreAbsoluteCleanAndBounded(t *testing.T) {
	if err := ValidateSettings("vless", certificateFiles(), true); err != nil {
		t.Fatalf("a legal file pair was refused: %v", err)
	}
	cases := map[string]struct{ certificate, key string }{
		"relative":         {"certs/a.crt", "certs/a.key"},
		"escaping":         {CertificateDirectory + "/../x", CertificateDirectory + "/a.key"},
		"unclean":          {CertificateDirectory + "/./a.crt", CertificateDirectory + "/a.key"},
		"sibling":          {CertificateDirectory + "X/a.crt", CertificateDirectory + "X/a.key"},
		"too long":         {CertificateDirectory + "/" + strings.Repeat("a", MaxCertificatePathLength), CertificateDirectory + "/a.key"},
		"certificate gone": {"", CertificateDirectory + "/a.key"},
		"key missing":      {CertificateDirectory + "/a.crt", ""},
		"same file":        {CertificateDirectory + "/a.crt", CertificateDirectory + "/a.crt"},
		"pasted as well":   {CertificateDirectory + "/a.crt", CertificateDirectory + "/a.key"},
	}
	for name, test := range cases {
		settings := ProtocolSettings{Security: "tls", CertificatePath: test.certificate, PrivateKeyPath: test.key}
		if name == "pasted as well" {
			settings.CertificatePEM, settings.PrivateKeyPEM = "certificate", "key"
		}
		if err := ValidateSettings("vless", settings, true); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

// Files belong to a TLS listener and nowhere else: not to an outbound, not to a
// Reality listener, and not to a listener without TLS.
func TestCertificateFilesBelongToATLSInbound(t *testing.T) {
	if err := ValidateSettings("vless", certificateFiles(), false); err == nil || !strings.Contains(err.Error(), "inbound") {
		t.Fatalf("an outbound accepted certificate files: %v", err)
	}
	reality := certificateFiles()
	reality.Security, reality.Reality = SecurityReality, testReality(t)
	if err := ValidateSettings("vless", reality, true); err == nil || !strings.Contains(err.Error(), "require TLS") {
		t.Fatalf("a Reality listener accepted certificate files: %v", err)
	}
	plain := certificateFiles()
	plain.Security = "none"
	if err := ValidateSettings("vless", plain, true); err == nil || !strings.Contains(err.Error(), "require TLS") {
		t.Fatalf("a listener without TLS accepted certificate files: %v", err)
	}
}

// The capability report is the only thing that authorizes the two paths, because
// an Agent that predates the field rejects them instead of rendering them.
func TestCertificateFilesNeedTheCoreReport(t *testing.T) {
	settings := certificateFiles()
	closed := CoreSettingsCapability{Core: "xray", Version: SettingsContractVersion}
	if err := RequireInboundServerSettings("xray", closed, "vless", settings); err == nil || !strings.Contains(err.Error(), "certificate file") {
		t.Fatalf("files were accepted without the report: %v", err)
	}
	open := CoreSettingsCapability{Core: "xray", Version: SettingsContractVersion, CertificateFiles: true}
	if err := RequireInboundServerSettings("xray", open, "vless", settings); err != nil {
		t.Fatalf("the declared certificate files were refused: %v", err)
	}
	if err := RequireInboundServerSettings("sing-box", open, "vless", settings); err == nil {
		t.Fatal("another core's report authorized the files")
	}
	if err := RequireInboundServerSettings("xray", CoreSettingsCapability{Core: "xray"}, "vless", ProtocolSettings{Security: "tls"}); err != nil {
		t.Fatalf("legacy TLS material needs no report: %v", err)
	}
}
