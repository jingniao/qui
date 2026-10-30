package configuration

import (
	"bytes"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/config"
	"testing"
)

func TestInventoryOverlayRecoveryAndAuthentication(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "configuration.key"), bytes.Repeat([]byte{7}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	c := config.Agent{DataDirectory: dir, CoreName: "xray", CorePath: "/usr/local/lib/proxy-agent/xray"}
	i, err := ReadInventory(dir, "node")
	if err != nil {
		t.Fatal(err)
	}
	i.Entries["xray"] = InventoryCore{Path: c.CorePath, Disabled: true, RequestID: "remove"}
	i.Entries["sing-box"] = InventoryCore{Path: filepath.Join(dir, "releases", "installed", "sing-box-image.bin")}
	if err = WriteInventory(dir, i); err != nil {
		t.Fatal(err)
	}
	// Interrupted temporary writes are never read as the committed inventory.
	if err = os.WriteFile(filepath.Join(dir, ".cores-interrupted"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	i, err = ReadInventory(dir, "node")
	if err != nil {
		t.Fatal(err)
	}
	merged, err := i.Merge(c)
	if err != nil || len(merged) != 1 || merged[0].Name != "sing-box" {
		t.Fatal(merged, err)
	}
	if _, err = ReadInventory(dir, "other-node"); err == nil {
		t.Fatal("foreign inventory accepted")
	}
	path := filepath.Join(dir, "cores.json.enc")
	data, _ := os.ReadFile(path)
	data[len(data)-1] ^= 1
	os.WriteFile(path, data, 0600)
	if _, err = ReadInventory(dir, "node"); err == nil {
		t.Fatal("corrupted inventory accepted")
	}
}

func TestInventoryKeepsPrimaryWhenOriginalCoreIsReenabled(t *testing.T) {
	c := config.Agent{DataDirectory: t.TempDir(), CoreName: "xray", CorePath: "/installer/xray"}
	i := Inventory{NodeID: "node", Primary: "sing-box", Entries: map[string]InventoryCore{"sing-box": {Path: "/managed/sing-box"}}}
	cores, err := i.Merge(c)
	if err != nil || len(cores) != 2 || cores[0].Name != "sing-box" {
		t.Fatal("reenabling original core stole primary", cores, err)
	}
	i.Primary = "xray"
	cores, err = i.Merge(c)
	if err != nil || cores[0].Name != "xray" {
		t.Fatal(cores, err)
	}
}
