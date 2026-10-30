package configuration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/vault"
	"sort"
)

// Inventory is an authenticated overlay. YAML and the install record remain
// installer-owned. Disabled entries override YAML; additions never rewrite it.
type Inventory struct {
	NodeID                 string                   `json:"node_id"`
	Primary                string                   `json:"primary,omitempty"`
	PrimaryChangedRevision int64                    `json:"primary_changed_revision,omitempty"`
	Entries                map[string]InventoryCore `json:"entries"`
}
type InventoryCore struct {
	Path            string `json:"path"`
	Disabled        bool   `json:"disabled,omitempty"`
	RemoveUnacked   bool   `json:"remove_unacked,omitempty"`
	RequestID       string `json:"request_id,omitempty"`
	Revision        int64  `json:"revision,omitempty"`
	Version         string `json:"version,omitempty"`
	Digest          string `json:"digest,omitempty"`
	InstalledDigest string `json:"installed_digest,omitempty"`
	ImageBytes      int64  `json:"image_bytes,omitempty"`
	URL             string `json:"url,omitempty"`
	Sequence        int64  `json:"sequence,omitempty"`
	Generation      string `json:"generation,omitempty"`
}

func ReadInventory(directory, nodeID string) (Inventory, error) {
	i := Inventory{NodeID: nodeID, Entries: map[string]InventoryCore{}}
	data, err := os.ReadFile(filepath.Join(directory, "cores.json.enc"))
	if errors.Is(err, os.ErrNotExist) {
		return i, nil
	}
	if err != nil {
		return i, err
	}
	v, err := inventoryVault(directory)
	if err != nil {
		return i, err
	}
	data, err = v.Open(data)
	if err != nil {
		return i, err
	}
	if err = json.Unmarshal(data, &i); err != nil {
		return i, err
	}
	if (nodeID != "" && i.NodeID != nodeID) || i.Entries == nil || len(i.Entries) > 2 {
		return i, errors.New("invalid core inventory identity")
	}
	for name, e := range i.Entries {
		if (name != "xray" && name != "sing-box") || e.Path == "" || (e.Generation != "" && (filepath.Base(e.Generation) != e.Generation || e.Generation == "." || e.Generation == "..")) {
			return i, errors.New("invalid core inventory")
		}
	}
	return i, nil
}
func inventoryVault(directory string) (*vault.Vault, error) {
	key, err := os.ReadFile(filepath.Join(directory, "configuration.key"))
	if err != nil {
		return nil, err
	}
	defer clear(key)
	return vault.FromKey(key)
}
func WriteInventory(directory string, i Inventory) error {
	v, err := inventoryVault(directory)
	if err != nil {
		return err
	}
	data, err := json.Marshal(i)
	if err != nil {
		return err
	}
	data, err = v.Seal(data)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(directory, ".cores-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(directory, "cores.json.enc")); err != nil {
		return err
	}
	return vault.SyncDir(directory)
}
func (i Inventory) Merge(c config.Agent) ([]Core, error) {
	result := []Core{}
	seen := map[string]bool{}
	for _, e := range Cores(c) {
		seen[e.Name] = true
		if o, ok := i.Entries[e.Name]; ok {
			if o.Disabled {
				continue
			}
			e.Path = o.Path
		}
		result = append(result, e)
	}
	names := []string{}
	for n := range i.Entries {
		if !seen[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		e := i.Entries[n]
		if !e.Disabled {
			result = append(result, Core{Name: n, Path: e.Path})
		}
	}
	if len(result) == 0 {
		return nil, errors.New("core inventory disables the last core")
	}
	if i.Primary != "" {
		found := false
		for index, entry := range result {
			if entry.Name == i.Primary {
				copy(result[1:index+1], result[:index])
				result[0] = entry
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("inventory primary is not enabled")
		}
	}
	return result, nil
}
