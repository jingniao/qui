package desiredstate

import (
	"errors"
	"path"
	"strings"
)

// Node-side certificate files (v1.4.7 section 3, phase C1).
//
// A TLS inbound may name a certificate and key file that already exist on the
// node instead of carrying pasted PEM material. The paths are panel-side
// metadata, so they travel in the settings the Agent renders; the Agent reads
// the files itself and the private key never reaches the panel.
const (
	// CertificateDirectory is the only directory a node may read certificate
	// files from. The Agent resolves every path and refuses anything outside it,
	// so a compromised panel cannot ask the Agent to read arbitrary files.
	CertificateDirectory = "/etc/proxy-agent/certs"
	// MaxCertificatePathLength keeps one path inside a bounded settings document.
	MaxCertificatePathLength = 256
)

// HasCertificateFiles reports whether the settings name node-side certificate
// files at all, so no layer has to test the two fields in its own order.
func (s ProtocolSettings) HasCertificateFiles() bool {
	return s.CertificatePath != "" || s.PrivateKeyPath != ""
}

// validateCertificateFiles checks the node-side certificate files of one settings
// block. Every rule lives here so the panel (save, snapshot, publish) and the
// Agent (render) cannot disagree about what a legal path is.
//
// The comparison uses path, not filepath: these are Linux node paths, and the
// checks must behave the same on a developer's Windows host.
func validateCertificateFiles(settings ProtocolSettings, inbound bool) error {
	if !settings.HasCertificateFiles() {
		return nil
	}
	if !inbound {
		// An outbound presents a client identity, never a server certificate.
		return errors.New("certificate files belong to an inbound")
	}
	if settings.Security != "tls" {
		// Reality authenticates against a real site instead of presenting a
		// certificate, so certificate files on it are a contradiction.
		return errors.New("certificate files require TLS")
	}
	if settings.CertificatePEM != "" || settings.PrivateKeyPEM != "" {
		return errors.New("certificate files and pasted TLS material are mutually exclusive")
	}
	if settings.CertificatePath == "" || settings.PrivateKeyPath == "" {
		return errors.New("both the certificate and the private key file are required")
	}
	if settings.CertificatePath == settings.PrivateKeyPath {
		return errors.New("the certificate and the private key file must differ")
	}
	for _, value := range []string{settings.CertificatePath, settings.PrivateKeyPath} {
		if len(value) > MaxCertificatePathLength {
			return errors.New("certificate path too long")
		}
		// An absolute, already clean path is required, so one file has exactly
		// one spelling: a path that could be cleaned differently is refused
		// instead of being normalized here.
		if !path.IsAbs(value) || path.Clean(value) != value {
			return errors.New("certificate path must be absolute and already clean")
		}
		if !strings.HasPrefix(value, CertificateDirectory+"/") {
			return errors.New("certificate path must be under " + CertificateDirectory)
		}
	}
	return nil
}
