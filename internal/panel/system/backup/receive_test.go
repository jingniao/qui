package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type failingReceiveReader struct {
	data    []byte
	failure error
}

func (r *failingReceiveReader) Read(buffer []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(buffer, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.failure
}

func TestReceiveRequiresCompleteVerifiedArchiveBeforePublicationAndCleansAllFailures(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	created, err := source.Create(context.Background(), directory, "receive_fixture_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, created.Manifest.ID+".pcb"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"success", "read-error", "cancel", "bad-password", "short", "callback"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			ctx := context.Background()
			password := testPassword
			var input io.Reader = bytes.NewReader(data)
			calls := 0
			failure := errors.New("transport interrupted")
			switch fault {
			case "read-error":
				input = &failingReceiveReader{data: data, failure: failure}
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "bad-password":
				password = "wrong-upload-password"
			case "short":
				input = bytes.NewReader([]byte("short"))
			}
			result, err := Receive(ctx, root, input, password, func(result Result, path string) error {
				calls++
				if result.SHA256 != created.SHA256 || result.Manifest.ID != created.Manifest.ID {
					t.Fatal("callback received unverified result")
				}
				stored, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(stored, data) {
					t.Fatal("callback staging bytes changed", err)
				}
				if fault == "callback" {
					return failure
				}
				return nil
			})
			if fault == "success" {
				if err != nil || calls != 1 || result.SHA256 != created.SHA256 {
					t.Fatal(result, calls, err)
				}
			} else {
				if err == nil {
					t.Fatal("invalid receive reported success", fault)
				}
				if fault != "callback" && calls != 0 {
					t.Fatal("invalid receive invoked publication", fault, calls)
				}
			}
			assertNoStage(t, root)
			if files, err := os.ReadDir(root); err != nil || len(files) != 0 {
				t.Fatal("receive left file after staging cleanup", len(files), err)
			}
		})
	}
}
