package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifiedExaminationOnlyCallsReaderAfterCompleteAuthenticationAndCleansCallbackFailure(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	created, err := source.Create(ctx, directory, "examine_fixture_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, created.Manifest.ID+".pcb")
	calls := 0
	if _, err := Examine(ctx, directory, path, "wrong-examine-password", func(Result, string) error { calls++; return nil }); !errors.Is(err, ErrArchive) || calls != 0 {
		t.Fatal("reader invoked before authentication", calls, err)
	}
	failure := errors.New("restore reader failed")
	if _, err := Examine(ctx, directory, path, testPassword, func(result Result, work string) error {
		calls++
		if result.SHA256 != created.SHA256 {
			t.Fatal("unverified archive identity")
		}
		inventory, err := InventoryFromVerifiedDirectory(ctx, work)
		if err != nil {
			t.Fatal(err)
		}
		if inventory.Totals.Users != 1 || inventory.Totals.Devices != 1 || inventory.Totals.Nodes != 1 || inventory.Totals.Revisions != 1 {
			t.Fatal("validated inventory did not reflect database", inventory.Totals)
		}
		return failure
	}); !errors.Is(err, failure) || calls != 1 {
		t.Fatal("callback failure swallowed", calls, err)
	}
	assertNoStage(t, directory)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Examine(ctx, directory, path, testPassword, func(Result, string) error { calls++; return nil }); err == nil || calls != 1 {
		t.Fatal("reader invoked on damaged end marker", calls, err)
	}
	assertNoStage(t, directory)
}
