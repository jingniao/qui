package agenthost

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"proxycontrol/internal/platform/config"
	"proxycontrol/pkg/contracts/release"
)

// CataloguePortFile records the panel catalogue port from the last Welcome (0600, JSON {"port":N}).
const CataloguePortFile = "catalogue.json"

type cataloguePortRecord struct {
	Port uint32 `json:"port"`
}

// SaveCataloguePort writes or removes the catalogue port file. Port 0 removes the file.
func SaveCataloguePort(dataDirectory string, port uint32) error {
	target := filepath.Join(dataDirectory, CataloguePortFile)
	if port == 0 {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := json.Marshal(cataloguePortRecord{Port: port})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(target, data, 0600)
}

// EffectiveUpdates returns c.Updates when it is Enabled(); otherwise the panel catalogue
// https://<control host>:<saved port>/agent-catalogue/ with CA = c.TLS.ClientCA and
// keys = release.OfficialKeyPins. ok is false when no port has been saved.
func EffectiveUpdates(c config.Agent) (updates config.Updates, ok bool, err error) {
	if c.Updates.Enabled() {
		return c.Updates, true, nil
	}
	if c.DataDirectory == "" {
		return config.Updates{}, false, nil
	}
	target := filepath.Join(c.DataDirectory, CataloguePortFile)
	data, err := os.ReadFile(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config.Updates{}, false, nil
		}
		return config.Updates{}, false, err
	}
	var record cataloguePortRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return config.Updates{}, false, err
	}
	if record.Port == 0 || record.Port > 65535 {
		return config.Updates{}, false, nil
	}

	host := c.ControlAddress
	if h, _, err := net.SplitHostPort(c.ControlAddress); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	hostPort := net.JoinHostPort(host, strconv.FormatUint(uint64(record.Port), 10))

	return config.Updates{
		Catalogue: fmt.Sprintf("https://%s/agent-catalogue/", hostPort),
		CA:        c.TLS.ClientCA,
		Keys:      release.OfficialKeyPins,
	}, true, nil
}
