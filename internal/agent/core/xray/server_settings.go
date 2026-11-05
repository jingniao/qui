package xray

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// This is the Agent's renderer contract, separately from the core's VLESS flag.
// The advertised set is tested against pinned Xray 26.3.27. Detection also runs
// the installed binary's native parser; the version banner alone grants nothing.
func serverSettingsCapability() desiredstate.CoreSettingsCapability {
	combinations := []desiredstate.SettingsCombination{}
	for _, pair := range [][2]string{{"reality", "tcp"}, {"reality", "grpc"}, {"reality", "xhttp"}, {"tls", "xhttp"}} {
		combinations = append(combinations, desiredstate.SettingsCombination{Protocol: "vless", Direction: desiredstate.DirectionInbound, Security: pair[0], Transport: pair[1]})
	}
	combinations = append(combinations, desiredstate.SettingsCombination{Protocol: "vless", Direction: desiredstate.DirectionInbound, Security: "reality", Transport: "tcp", Flow: "xtls-rprx-vision"})
	return desiredstate.CoreSettingsCapability{Core: "xray", Version: desiredstate.SettingsContractVersion, Combinations: combinations}
}

func validateServerCombinations(state desiredstate.State) error {
	for _, inbound := range state.Inbounds {
		if err := desiredstate.RequireInboundServerSettings("xray", serverSettingsCapability(), inbound.Protocol, inbound.Settings); err != nil {
			return err
		}
	}
	return nil
}

func detectServerSettings(ctx context.Context, executable, version string) []*pb.SettingsCombination {
	return probeServerSettings(ctx, version, func(ctx context.Context, path string) error {
		command := exec.CommandContext(ctx, executable, "run", "-test", "-config", path)
		command.Stdout, command.Stderr = io.Discard, io.Discard
		return command.Run()
	})
}

func probeServerSettings(ctx context.Context, version string, check func(context.Context, string) error) []*pb.SettingsCombination {
	if !versionAtLeast(version, [3]int{26, 3, 27}) {
		return nil
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil
	}
	pub, tlsKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "panel.invalid"}, DNSNames: []string{"panel.invalid"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, pub, tlsKey)
	if err != nil {
		return nil
	}
	pk, err := x509.MarshalPKCS8PrivateKey(tlsKey)
	if err != nil {
		return nil
	}
	defer clear(pk)
	certificatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}))
	directory, err := os.MkdirTemp("", "qui-xray-settings-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "probe.json")
	result := []*pb.SettingsCombination{}
	for _, c := range serverSettingsCapability().Combinations {
		settings := desiredstate.ProtocolSettings{Security: c.Security, Transport: c.Transport, Flow: c.Flow}
		if c.Security == "reality" {
			settings.Reality = &desiredstate.RealityServerSettings{PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), HandshakeTarget: "127.0.0.1:443", ServerNames: []string{"panel.invalid"}, ShortIDs: []string{"01"}}
		}
		if c.Security == "tls" {
			settings.CertificatePEM, settings.PrivateKeyPEM = certificatePEM, privatePEM
		}
		if c.Transport == "grpc" {
			settings.ServiceName = "probe"
		}
		modes := []string{""}
		if c.Transport == "xhttp" {
			modes = []string{"auto", "packet-up", "stream-up", "stream-one"}
		}
		passed := true
		for _, mode := range modes {
			if c.Transport == "xhttp" {
				settings.XHTTP = &desiredstate.XHTTPSettings{Host: "panel.invalid", Path: "/probe", Mode: mode}
			}
			transport, err := stream(settings, true)
			if err != nil {
				passed = false
				break
			}
			document := configDocument{Log: logConfig{Access: "none", Error: "none", Level: "none"}, Inbounds: []inboundConfig{{Tag: "settings-probe", Listen: "127.0.0.1", Port: 1, Protocol: "vless", Settings: vlessInbound{Clients: []vlessAccount{{ID: "6f1a1d4e-2f4a-4a4f-9d6e-3a1c0b7e5d21", Flow: c.Flow}}, Decryption: "none"}, Stream: transport}}, Outbounds: []outboundConfig{{Tag: "direct", Protocol: "freedom"}}, Routing: routingConfig{DomainStrategy: "AsIs", Rules: []route{}}}
			data, err := json.Marshal(document)
			if err != nil || os.WriteFile(path, data, 0600) != nil {
				clear(data)
				passed = false
				break
			}
			clear(data)
			timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = check(timeout, path)
			cancel()
			if err != nil {
				passed = false
				break
			}
		}
		if !passed {
			continue
		}
		result = append(result, &pb.SettingsCombination{Protocol: c.Protocol, Direction: c.Direction, Security: c.Security, Transport: c.Transport, Flow: c.Flow})
	}
	return result
}

var errServerSettingsUnavailable = errors.New("installed Xray has not passed the xhttp/Reality settings probe")
