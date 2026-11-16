package core

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"

	"proxycontrol/pkg/contracts/desiredstate"
)

// ProbeClientSettings returns valid sample settings for one outbound client
// combination, using every client field the shape admits, so an adapter can run
// its renderer and the core's native check on it before reporting the
// combination (v1.6.0). The values never leave the probe.
func ProbeClientSettings(c desiredstate.SettingsCombination) (desiredstate.ProtocolSettings, error) {
	settings := desiredstate.ProtocolSettings{Security: c.Security, Transport: c.Transport, Flow: c.Flow}
	switch c.Security {
	case "tls":
		settings.ServerName, settings.ALPN, settings.ClientFingerprint = "panel.invalid", []string{"h2"}, "chrome"
	case desiredstate.SecurityReality:
		key, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return settings, err
		}
		settings.ServerName, settings.ClientFingerprint = "panel.invalid", "chrome"
		settings.RealityClient = &desiredstate.RealityClientSettings{PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortID: "01", SpiderX: "/"}
	}
	switch c.Transport {
	case "ws":
		settings.Path, settings.WSHost = "/probe", "panel.invalid"
	case "grpc":
		settings.ServiceName = "probe"
	case desiredstate.TransportXHTTP:
		settings.XHTTP = &desiredstate.XHTTPSettings{Host: "panel.invalid", Path: "/probe", Mode: desiredstate.XHTTPModeAuto}
	}
	return settings, desiredstate.ValidateSettings(c.Protocol, settings, false)
}
