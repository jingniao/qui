//go:build windows

package identity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestLoadRetriesOnlyTransientWindowsSharingConflict(t *testing.T) {
	for _, releaseLock := range []bool{true, false} {
		name := "released"
		if !releaseLock {
			name = "held"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			state := &State{NodeID: "original-node", Directory: directory}
			if err := state.Save(42); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "identity.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			wide, err := windows.UTF16PtrFromString(path)
			if err != nil {
				t.Fatal(err)
			}
			handle, err := windows.CreateFile(wide, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.ReadFile(path); !sharingConflict(err) {
				windows.CloseHandle(handle)
				t.Fatal("fixture did not create a sharing conflict", err)
			}
			done := make(chan error, 1)
			if releaseLock {
				go func() {
					time.Sleep(35 * time.Millisecond)
					done <- windows.CloseHandle(handle)
				}()
			} else {
				defer windows.CloseHandle(handle)
			}
			started := time.Now()
			loaded, err := Load(directory)
			if time.Since(started) > time.Second {
				t.Fatal("identity read exceeded bounded retries")
			}
			if !releaseLock {
				if loaded != nil || !sharingConflict(err) {
					t.Fatal("persistent sharing conflict was hidden", loaded, err)
				}
				return
			}
			if closeErr := <-done; closeErr != nil {
				t.Fatal(closeErr)
			}
			if err != nil || loaded.NodeID != state.NodeID || loaded.LastAcked != 42 {
				t.Fatal("transient conflict lost original identity or ACK", loaded, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) {
				t.Fatal("read changed identity", err)
			}
		})
	}
}

func TestSharingConflictDoesNotIncludeOtherIdentityErrors(t *testing.T) {
	for _, err := range []error{nil, os.ErrNotExist, os.ErrPermission, errors.New("corrupt identity"), windows.ERROR_ACCESS_DENIED} {
		if sharingConflict(err) {
			t.Fatal("non-transient error classified as sharing conflict", err)
		}
	}
}
