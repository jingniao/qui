package reconcile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"proxycontrol/internal/agent/core"
)

type componentHold struct {
	Component string `json:"component"`
	NodeID    string `json:"node_id"`
	Revision  int64  `json:"revision"`
}

func (r *Reconciler) componentHoldPath(name string) string {
	return filepath.Join(r.Directory, "core-update-"+name+"-hold.enc")
}
func (r *Reconciler) loadComponentHolds() {
	r.componentHolds, r.componentHoldsInvalid = map[string]bool{}, map[string]bool{}
	for _, name := range []string{"xray", "sing-box"} {
		data, err := readInspectionFile(r.componentHoldPath(name), 4096)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		r.componentHolds[name] = true
		if err != nil {
			r.componentHoldsInvalid[name] = true
			continue
		}
		plain, err := r.vault.Open(data)
		var marker componentHold
		if err != nil || json.Unmarshal(plain, &marker) != nil || marker.Component != name || marker.NodeID != r.NodeID || marker.Revision != r.manifest.Revision {
			r.componentHoldsInvalid[name] = true
		}
	}
}
func (r *Reconciler) anyCoreHold() bool {
	if r.coreUpdateHold {
		return true
	}
	for _, held := range r.componentHolds {
		if held {
			return true
		}
	}
	return false
}
func (r *Reconciler) holdComponent(name string) error {
	if name != "xray" && name != "sing-box" {
		return errors.New("unknown component")
	}
	r.componentHolds[name] = true
	if r.componentHoldsInvalid[name] {
		return core.ErrReleaseRecovery
	}
	data, err := json.Marshal(componentHold{Component: name, NodeID: r.NodeID, Revision: r.manifest.Revision})
	if err != nil {
		return err
	}
	sealed, err := r.vault.Seal(data)
	if err != nil {
		return err
	}
	if err := writeFile(r.componentHoldPath(name), sealed); err != nil {
		return err
	}
	return syncReleaseHoldDirectory(r.Directory)
}
func (r *Reconciler) releaseComponent(name string) error {
	if r.componentHoldsInvalid[name] {
		return core.ErrReleaseRecovery
	}
	if err := os.Remove(r.componentHoldPath(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncReleaseHoldDirectory(r.Directory); err != nil {
		return err
	}
	r.componentHolds[name] = false
	return nil
}
func (r *Reconciler) CoreComponentHeld(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.coreUpdateHold || r.componentHolds[name] || r.controlOnly
}
