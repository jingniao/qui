package backup

import (
	"context"
	"errors"
	"testing"
)

func TestValidateNodeTags(t *testing.T) {
	t.Parallel()
	source, _, _ := sourceFixture(t)
	ctx := context.Background()
	if _, err := source.Database.ExecContext(ctx, `INSERT INTO node_meta(node_id,tags_json) SELECT id,'["香港","CN2"]' FROM nodes LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	if err := validateNodeTags(ctx, source.Database); err != nil {
		t.Fatalf("valid tags rejected: %v", err)
	}
	for _, bad := range []string{`["a","A"]`, `[" a"]`, `["a,b"]`, `[""]`} {
		if _, err := source.Database.ExecContext(ctx, "UPDATE node_meta SET tags_json=?", bad); err != nil {
			t.Fatal(err)
		}
		if err := validateNodeTags(ctx, source.Database); !errors.Is(err, ErrArchive) {
			t.Fatalf("%s accepted: %v", bad, err)
		}
	}
	// An archive written before 0144 has no column and is valid.
	rewindNodeTags(t, source.Database)
	if err := validateNodeTags(ctx, source.Database); err != nil {
		t.Fatalf("pre-0144 archive rejected: %v", err)
	}
}
