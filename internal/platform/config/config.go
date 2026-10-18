package config

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"proxycontrol/pkg/contracts/release"
)

type TLS struct {
	Certificate string `yaml:"certificate"`
	Key         string `yaml:"key"`
	ClientCA    string `yaml:"client_ca"`
}

type Panel struct {
	Development           bool             `yaml:"development"`
	HTTPAddress           string           `yaml:"http_address"`
	GRPCAddress           string           `yaml:"grpc_address"`
	Database              string           `yaml:"database"`
	NodeIdentityDirectory string           `yaml:"node_identity_directory"`
	TLS                   TLS              `yaml:"tls"`
	ShutdownTimeout       time.Duration    `yaml:"shutdown_timeout"`
	ReleaseKeys           []release.KeyPin `yaml:"release_keys,omitempty"`
	Server                Server           `yaml:"server"`
}

// Server holds HTTP front-end settings. TrustedProxies lists the peers whose
// X-Forwarded-For / X-Real-IP headers the panel may trust; an empty list means
// the direct peer address is always used.
type Server struct {
	TrustedProxies []string `yaml:"trusted_proxies,omitempty"`
}

// ParsedTrustedProxies resolves the configured peers to networks. A bare IP is
// accepted and treated as a single-host network.
func (s Server) ParsedTrustedProxies() ([]*net.IPNet, error) {
	if len(s.TrustedProxies) == 0 {
		return nil, nil
	}
	networks := make([]*net.IPNet, 0, len(s.TrustedProxies))
	for _, value := range s.TrustedProxies {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(value); err == nil {
			networks = append(networks, network)
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			bits := 128
			if ip.To4() != nil {
				ip, bits = ip.To4(), 32
			}
			networks = append(networks, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		return nil, fmt.Errorf("server.trusted_proxies: %q is not a valid IP or CIDR", value)
	}
	return networks, nil
}

type Agent struct {
	Development    bool    `yaml:"development"`
	ControlAddress string  `yaml:"control_address"`
	ServerName     string  `yaml:"server_name"`
	DataDirectory  string  `yaml:"data_directory"`
	CorePath       string  `yaml:"core_path"`
	CoreName       string  `yaml:"core_name"`
	TLS            TLS     `yaml:"tls"`
	Updates        Updates `yaml:"updates"`
}

// Updates is operator-pinned configuration. Control messages cannot change
// the catalogue, trusted keys, key purposes or certificate roots.
type Updates struct {
	Catalogue string           `yaml:"catalogue"`
	CA        string           `yaml:"ca"`
	Keys      []release.KeyPin `yaml:"keys"`
}

func (u Updates) Enabled() bool { return u.Catalogue != "" || u.CA != "" || len(u.Keys) != 0 }
func (u Updates) Validate() error {
	if !u.Enabled() {
		return nil
	}
	if _, err := release.CatalogueURL(u.Catalogue); err != nil {
		return err
	}
	if _, err := release.PinnedKeys(u.Keys); err != nil {
		return err
	}
	_, err := u.TrustConfig()
	return err
}
func (u Updates) TrustConfig() (*tls.Config, error) {
	c := &tls.Config{MinVersion: tls.VersionTLS13}
	if u.CA != "" {
		roots, err := (TLS{ClientCA: u.CA}).roots()
		if err != nil {
			return nil, err
		}
		c.RootCAs = roots
	}
	return c, nil
}

func DefaultPanel() Panel {
	return Panel{Development: true, HTTPAddress: "127.0.0.1:8080", GRPCAddress: "127.0.0.1:9443", Database: "state/panel.db", NodeIdentityDirectory: "state/panel-identity", ShutdownTimeout: 10 * time.Second}
}

func DefaultAgent() Agent {
	return Agent{Development: true, ControlAddress: "127.0.0.1:9443", DataDirectory: "state/agent", CorePath: "xray", TLS: TLS{ClientCA: "state/panel-identity/ca.crt"}}
}

func decode(path string, target any) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read configuration: %w", err)
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(target); err != nil {
		return fmt.Errorf("decode configuration: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("configuration must contain exactly one YAML document")
	}
	return nil
}

func LoadPanel(path string) (Panel, error) {
	c, err := LoadPanelPaths(path)
	if err != nil {
		return c, err
	}
	return c, c.Validate()
}

// LoadPanelPaths resolves strict configuration paths without loading TLS. The
// panel startup must first finish an interrupted file recovery under lifetime
// ownership, then validate the complete restored TLS/configuration set.
func LoadPanelPaths(path string) (Panel, error) {
	c := DefaultPanel()
	if err := decode(path, &c); err != nil {
		return c, err
	}
	resolve := resolver(path)
	c.Database = resolve(c.Database)
	c.NodeIdentityDirectory = resolve(c.NodeIdentityDirectory)
	c.TLS.resolve(resolve)
	return c, nil
}

func LoadAgent(path string) (Agent, error) {
	c := DefaultAgent()
	if err := decode(path, &c); err != nil {
		return c, err
	}
	c.DataDirectory = resolver(path)(c.DataDirectory)
	c.TLS.resolve(resolver(path))
	c.Updates.CA = resolver(path)(c.Updates.CA)
	return c, c.Validate()
}

func resolver(path string) func(string) string {
	return func(value string) string {
		if value == "" || filepath.IsAbs(value) || path == "" {
			return value
		}
		return filepath.Join(filepath.Dir(path), value)
	}
}

func (t *TLS) resolve(resolve func(string) string) {
	t.Certificate, t.Key, t.ClientCA = resolve(t.Certificate), resolve(t.Key), resolve(t.ClientCA)
}

func validateAddress(address string, loopbackOnly bool) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return fmt.Errorf("invalid address %q: require host:port", address)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 || !loopbackOnly && portNumber == 0 {
		return errors.New("port must be a valid numeric TCP port")
	}
	if loopbackOnly {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("development mode requires a literal loopback IP address")
		}
	}
	return nil
}

func (c Panel) Validate() error {
	if err := c.ValidateStructure(); err != nil {
		return err
	}
	if !c.Development {
		_, err := c.TLS.ServerConfig()
		return err
	}
	return nil
}

func (c Panel) ValidateStructure() error {
	if len(c.ReleaseKeys) != 0 {
		if _, err := release.PinnedKeys(c.ReleaseKeys); err != nil {
			return fmt.Errorf("release_keys: %w", err)
		}
	}
	if err := validateAddress(c.HTTPAddress, c.Development); err != nil {
		return fmt.Errorf("http_address: %w", err)
	}
	if err := validateAddress(c.GRPCAddress, c.Development); err != nil {
		return fmt.Errorf("grpc_address: %w", err)
	}
	if c.Database == "" {
		return errors.New("database path is required")
	}
	if strings.HasPrefix(c.Database, `\\`) || strings.HasPrefix(c.Database, "//") || strings.Contains(c.Database, "://") {
		return errors.New("SQLite database must be on a local filesystem path")
	}
	if c.NodeIdentityDirectory == "" {
		return errors.New("node_identity_directory is required")
	}
	if c.ShutdownTimeout < time.Second || c.ShutdownTimeout > time.Minute {
		return errors.New("shutdown_timeout must be between 1s and 1m")
	}
	if _, err := c.Server.ParsedTrustedProxies(); err != nil {
		return err
	}
	return nil
}

func (c Agent) Validate() error {
	if err := c.Updates.Validate(); err != nil {
		return fmt.Errorf("updates: %w", err)
	}
	if c.CoreName != "" && c.CoreName != "xray" && c.CoreName != "sing-box" {
		return errors.New("core_name must be xray or sing-box")
	}
	if err := validateAddress(c.ControlAddress, c.Development); err != nil {
		return err
	}
	if c.DataDirectory == "" {
		return errors.New("data_directory is required")
	}
	if !c.Development {
		_, err := c.TLS.ServerTrustConfig(c.ServerName)
		return err
	}
	return nil
}

func (t TLS) certificate() (tls.Certificate, error) {
	if t.Certificate == "" || t.Key == "" {
		return tls.Certificate{}, errors.New("TLS certificate and key are required outside development mode")
	}
	cert, err := tls.LoadX509KeyPair(t.Certificate, t.Key)
	if err != nil {
		return cert, fmt.Errorf("load TLS certificate: %w", err)
	}
	return cert, nil
}

func (t TLS) roots() (*x509.CertPool, error) {
	if t.ClientCA == "" {
		return nil, errors.New("TLS client_ca is required")
	}
	data, err := os.ReadFile(t.ClientCA)
	if err != nil {
		return nil, fmt.Errorf("read CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(data) {
		return nil, errors.New("CA file contains no certificates")
	}
	return roots, nil
}

func (t TLS) ServerConfig() (*tls.Config, error) {
	cert, err := t.certificate()
	if err != nil {
		return nil, err
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}
	if t.ClientCA != "" {
		roots, err := t.roots()
		if err != nil {
			return nil, err
		}
		config.ClientCAs = roots
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return config, nil
}

func (t TLS) ClientConfig(serverName string) (*tls.Config, error) {
	cert, err := t.certificate()
	if err != nil {
		return nil, err
	}
	roots, err := t.roots()
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, RootCAs: roots, ServerName: serverName}, nil
}

func (t TLS) ServerTrustConfig(serverName string) (*tls.Config, error) {
	roots, err := t.roots()
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: serverName}, nil
}
