package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"

	"proxycontrol/internal/panel/node"
)

// releaseVersion is the version form scripts/build-release.sh publishes and the
// one-command install downloads. A local build reports "dev" and therefore gets
// no install command in the register dialog.
var releaseVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?$`)

// Version is the panel release version, assigned by cmd/panel from the
// -X main.version build flag. It stays empty for a plain `go run`, and the
// register dialog then offers no install command.
var Version string

// installCertificate returns the PEM block the panel publishes as the trust
// anchor of its own web and gRPC certificate: the last CERTIFICATE block of the
// configured file, which is the top of the chain the listeners present. It is
// the only place those bytes are picked, so GET /install/ca.pem and the digest
// the enrollment answer carries always describe the same bytes.
func (s *Server) installCertificate() ([]byte, error) {
	if s.Config.Development {
		return nil, errors.New("development mode has no panel certificate to trust")
	}
	data, err := os.ReadFile(s.Config.TLS.Certificate)
	if err != nil {
		return nil, err
	}
	var anchor []byte
	for rest := data; ; {
		block, remainder := pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			anchor = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes})
		}
		rest = remainder
	}
	if anchor == nil {
		return nil, errors.New("panel certificate file contains no CERTIFICATE block")
	}
	return anchor, nil
}

// installInfo answers what the register dialog needs to build the one-command
// install. A field the panel cannot supply stays empty; enrollment itself never
// depends on this.
func (s *Server) installInfo() node.InstallInfo {
	info := node.InstallInfo{}
	if releaseVersion.MatchString(Version) {
		info.Version = Version
	}
	if _, port, err := net.SplitHostPort(s.Config.GRPCAddress); err == nil {
		if number, err := strconv.Atoi(port); err == nil && number >= 1 && number <= 65535 {
			info.GRPCPort = number
		}
	}
	anchor, err := s.installCertificate()
	if err != nil {
		return info
	}
	digest := sha256.Sum256(anchor)
	info.CASHA256 = hex.EncodeToString(digest[:])
	return info
}

// installCA serves the panel certificate trust anchor. It needs no session: the
// caller is a node that holds an enrollment token but no certificate yet, and
// the file is the public certificate the listeners already present.
func (s *Server) installCA(w http.ResponseWriter, r *http.Request) {
	anchor, err := s.installCertificate()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(anchor)
}
