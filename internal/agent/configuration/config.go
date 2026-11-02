// Package configuration owns the Agent's additive multi-core YAML format.
package configuration

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/platform/config"
)

type Core struct {
	Name string `yaml:"name" json:"name"`
	Path string `yaml:"path" json:"path"`
}

// Remembering the parsed list by the canonical state directory lets existing
// supervisor/worker APIs keep their legacy config.Agent signature. Every worker
// loads the original YAML itself, including managed worker subprocesses.
var lists sync.Map

// errGostPrimary refuses a GOST primary: GOST serves only forwards, and inbounds
// with an empty core follow the primary.
var errGostPrimary = errors.New("GOST cannot be the first core; list xray or sing-box first")

// Remember freezes the YAML primary before inventory overlays change scalar
// compatibility fields. Update-history ownership must not move with a removal.
func Remember(c config.Agent) {
	key, _ := filepath.Abs(c.DataDirectory)
	lists.LoadOrStore(key, Cores(c))
}

func Cores(c config.Agent) []Core {
	key, _ := filepath.Abs(c.DataDirectory)
	if v, ok := lists.Load(key); ok {
		return append([]Core(nil), v.([]Core)...)
	}
	name := c.CoreName
	if name == "" {
		name = "xray"
	}
	return []Core{{Name: name, Path: c.CorePath}}
}

func LoadAgent(path string) (config.Agent, error) {
	c := config.DefaultAgent()
	if path == "" {
		return c, c.Validate()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	doc := struct {
		config.Agent `yaml:",inline"`
		Cores        []Core `yaml:"cores"`
	}{Agent: c}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&doc); err != nil {
		return c, err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return c, errors.New("configuration must contain exactly one YAML document")
	}
	c = doc.Agent
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(filepath.Dir(path), p)
	}
	c.DataDirectory = resolve(c.DataDirectory)
	c.TLS.Certificate = resolve(c.TLS.Certificate)
	c.TLS.Key = resolve(c.TLS.Key)
	c.TLS.ClientCA = resolve(c.TLS.ClientCA)
	c.Updates.CA = resolve(c.Updates.CA)
	if doc.Cores != nil {
		if len(doc.Cores) == 0 || len(doc.Cores) > 3 {
			return c, errors.New("cores requires one to three cores")
		}
		seen := map[string]bool{}
		for _, entry := range doc.Cores {
			if !core.KnownName(entry.Name) || entry.Path == "" || seen[entry.Name] {
				return c, errors.New("invalid or duplicate core")
			}
			seen[entry.Name] = true
		}
		c.CoreName, c.CorePath = doc.Cores[0].Name, doc.Cores[0].Path
	}
	// Covers both the cores list and the legacy scalar core_name.
	if !core.CarriesUsers(c.CoreName) {
		return c, errGostPrimary
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	key, _ := filepath.Abs(c.DataDirectory)
	if doc.Cores == nil {
		lists.Delete(key)
	} else {
		lists.Store(key, append([]Core(nil), doc.Cores...))
	}
	return c, nil
}

func HasCores(c config.Agent) bool {
	key, _ := filepath.Abs(c.DataDirectory)
	_, ok := lists.Load(key)
	return ok
}
