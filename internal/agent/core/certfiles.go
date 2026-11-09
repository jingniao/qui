package core

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
)

// Node-side certificate files (v1.4.7 section 3, phase C1).
//
// A TLS inbound may name a certificate and key file that already exist on the
// node. The panel only stores the two paths, so the Agent is the first place that
// sees the files: it proves the core will be able to read them before the
// configuration is applied, and a failure fails the apply like any other compile
// or validation error (normal rollback rules).
//
// The private key never leaves the node: the renderers emit the core's own file
// fields and the core reads the files itself.

// CertificateDirectory is the directory a node's certificate files must live in.
// It is a variable only so a test can point this rule set at a fixture directory
// instead of the real root-owned path; production never assigns it.
var CertificateDirectory = desiredstate.CertificateDirectory

// ValidateCertificateFiles checks the node's certificate files for one inbound
// settings block. Settings without files pass, so every existing configuration is
// unaffected.
func ValidateCertificateFiles(settings desiredstate.ProtocolSettings) error {
	return checkCertificateFiles(settings.CertificatePath, settings.PrivateKeyPath, CertificateDirectory)
}

// checkCertificateFiles is the rule set, with the directory as a parameter so a
// test can exercise it against a fixture instead of the real root-owned path.
func checkCertificateFiles(certificatePath, keyPath, directory string) error {
	if certificatePath == "" && keyPath == "" {
		return nil
	}
	if certificatePath == "" || keyPath == "" {
		return errors.New("both the certificate and the private key file are required")
	}
	certificate, err := resolveCertificateFile(directory, certificatePath)
	if err != nil {
		return err
	}
	key, err := resolveCertificateFile(directory, keyPath)
	if err != nil {
		return err
	}
	pair, err := tls.LoadX509KeyPair(certificate, key)
	if err != nil {
		// The pair check covers readability, PEM shape and the key match.
		return fmt.Errorf("certificate and private key file do not form a usable pair: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("certificate file cannot be parsed: %w", err)
	}
	now := time.Now()
	switch {
	case now.After(leaf.NotAfter):
		return fmt.Errorf("certificate file expired at %s", leaf.NotAfter.UTC().Format(time.RFC3339))
	case now.Before(leaf.NotBefore):
		return fmt.Errorf("certificate file is not valid before %s", leaf.NotBefore.UTC().Format(time.RFC3339))
	}
	warnFileMode(key)
	return nil
}

// resolveCertificateFile maps one settings path onto the directory this Agent
// reads and returns the fully resolved file, refusing anything that leaves that
// directory or is not a regular file.
//
// Settings always carry the canonical node path (desiredstate.CertificateDirectory,
// which the panel validates), so the mapping is the identity on a node. It only
// moves when a test points CertificateDirectory at a fixture directory.
func resolveCertificateFile(directory, value string) (string, error) {
	base, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", errors.New("the node certificate directory " + desiredstate.CertificateDirectory + " is missing or unreadable")
	}
	local := filepath.Join(directory, strings.TrimPrefix(value, desiredstate.CertificateDirectory+"/"))
	resolved, err := filepath.EvalSymlinks(local)
	if err != nil {
		return "", errors.New("certificate file is missing or unreadable: " + value)
	}
	if !strings.HasPrefix(resolved, base+string(filepath.Separator)) {
		return "", errors.New("certificate file resolves outside " + desiredstate.CertificateDirectory + ": " + value)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", errors.New("certificate file is missing or unreadable: " + value)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("certificate file is not a regular file: " + value)
	}
	return resolved, nil
}

// warnFileMode reports a private key that other accounts can read. The Agent
// cannot refuse the file for this alone: the Owner may allow a group, and the
// pinned directory is already root-owned with 0750.
func warnFileMode(key string) {
	info, err := os.Stat(key)
	if err != nil {
		return
	}
	if perm := info.Mode().Perm(); perm&^fs.FileMode(0640) != 0 {
		slog.Warn("certificate private key is readable beyond its owner and group", "file", key, "mode", fmt.Sprintf("%04o", perm))
	}
}
