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
		if len(doc.Cores) == 0 || len(doc.Cores) > 2 {
			return c, errors.New("cores requires one or two cores")
		}
		seen := map[string]bool{}
		for _, core := range doc.Cores {
			if (core.Name != "xray" && core.Name != "sing-box") || core.Path == "" || seen[core.Name] {
				return c, errors.New("invalid or duplicate core")
			}
			seen[core.Name] = true
		}
		c.CoreName, c.CorePath = doc.Cores[0].Name, doc.Cores[0].Path
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
