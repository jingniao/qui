package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"proxycontrol/pkg/contracts/release"
)

type retainedDriver struct {
	node, path                         string
	held, stopped, activated, released bool
	failActivation                     bool
}

func (d *retainedDriver) Component() string                               { return "proxy-agent" }
func (d *retainedDriver) NodeID() string                                  { return d.node }
func (d *retainedDriver) Revision() int64                                 { return 1 }
func (d *retainedDriver) Current(context.Context) (string, string, error) { return d.path, "1.0", nil }
func (d *retainedDriver) Check(context.Context, string, string) error     { return nil }
func (d *retainedDriver) Hold() error                                     { d.held = true; return nil }
func (d *retainedDriver) Stop(context.Context) error                      { d.stopped = true; return nil }
func (d *retainedDriver) Release() error                                  { d.released = true; return nil }
func (d *retainedDriver) Activate(_ context.Context, path string) error {
	if !d.held || path != d.path {
		return ErrInstallation
	}
	d.activated = true
	if d.failActivation {
		return errors.New("health failure")
	}
	return nil
}

func TestRetainedAgentRecoveryKeepsHistoryAndFailsClosed(t *testing.T) {
	for _, phase := range []string{"ready", "committed", "rolled_back"} {
		for _, mode := range []string{"healthy", "unhealthy", "missing_image", "changed_image", "wrong_node"} {
			t.Run(phase+"/"+mode, func(t *testing.T) {
				options, signed, policy, private := historyFixture(t)
				h, err := CreateHistory(options)
				if err != nil {
					t.Fatal(err)
				}
				defer h.Close()
				ctx := context.Background()
				executable, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				image, err := InspectImage(ctx, "proxy-agent", executable, "1.0")
				if err != nil {
					t.Fatal(err)
				}
				directory := filepath.Join(t.TempDir(), "images")
				path, err := MaterializeImage(ctx, directory, executable, "proxy-agent", image)
				if err != nil {
					t.Fatal(err)
				}
				if err := h.BootstrapImage(ctx, "proxy-agent", image); err != nil {
					t.Fatal(err)
				}
				if phase != "ready" {
					manifest := signed.Manifest
					manifest.Component, manifest.SHA256, manifest.Bytes, manifest.Version = "proxy-agent", image.SHA256, image.Bytes, image.Version
					manifest.Sequence = 1
					signed, err = release.Sign(manifest, private)
					policy.Component = "proxy-agent"
					if err != nil {
						t.Fatal(err)
					}
					if err := h.Accept(ctx, signed, policy); err != nil {
						t.Fatal(err)
					}
					if _, err := h.BeginInstallation(ctx, "previous-install", 1, signed, policy); err != nil {
						t.Fatal(err)
					}
					if phase == "rolled_back" {
						if _, err := h.TransitionInstallation(ctx, "previous-install", "rolling_back"); err != nil {
							t.Fatal(err)
						}
					}
					if _, err := h.TransitionInstallation(ctx, "previous-install", phase); err != nil {
						t.Fatal(err)
					}
				}
				before, _, _ := h.Installation("proxy-agent")
				head := h.head
				d := &retainedDriver{node: options.NodeID, path: path}
				requested := before.Current
				switch mode {
				case "unhealthy":
					d.failActivation = true
				case "missing_image":
					if err := os.Rename(path, path+".missing"); err != nil {
						t.Fatal(err)
					}
				case "changed_image":
					requested.Version = "other"
				case "wrong_node":
					d.node = "other"
				}
				_, err = (&Installer{History: h, Directory: directory}).ReconcileRetained(ctx, d, requested)
				if (err == nil) != (mode == "healthy") {
					t.Fatal("unexpected outcome", mode, err)
				}
				if mode == "healthy" && (!d.activated || !d.released || d.stopped) {
					t.Fatal("healthy image was not released")
				}
				if (mode == "unhealthy" || mode == "missing_image") && (!d.stopped || d.released) {
					t.Fatal("failed recovery reopened worker")
				}
				if (mode == "changed_image" || mode == "wrong_node") && d.held {
					t.Fatal("unbound recovery touched worker")
				}
				after, _, _ := h.Installation("proxy-agent")
				if !reflect.DeepEqual(before, after) || h.head != head {
					t.Fatal("recovery rewrote installation history")
				}
			})
		}
	}
}
