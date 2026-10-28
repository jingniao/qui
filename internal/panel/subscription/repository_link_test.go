package subscription

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"proxycontrol/internal/platform/vault"
)

// C2 and A6.6 (the repository half): a token issued after 0114 keeps an encrypted
// copy, so the link can be shown again; a token without that copy (issued before
// v1.1) reports ErrNotRecoverable; and a reset revokes the old plaintext while the
// new link stays viewable.
func TestSubscriptionLinkRecoverableOnlyAfterMigration(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configurationVault, err := vault.Open(filepath.Join(t.TempDir(), "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	f.service.Repository.Vault = configurationVault

	_, plaintext, err := f.service.Rotate(ctx, f.u.ID, f.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	link, err := f.service.Repository.Link(ctx, f.u.ID)
	if err != nil || link != plaintext {
		t.Fatalf("link: %q %v", link, err)
	}

	// A row from before v1.1 has only the hash.
	if _, err := f.database.ExecContext(ctx, "UPDATE subscription_tokens SET encrypted_token=NULL WHERE user_id=? AND revoked_at IS NULL", f.u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Repository.Link(ctx, f.u.ID); !errors.Is(err, ErrNotRecoverable) {
		t.Fatalf("pre-1.1 link: %v", err)
	}

	_, renewed, err := f.service.Rotate(ctx, f.u.ID, f.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Repository.Resolve(ctx, plaintext); !errors.Is(err, ErrToken) {
		t.Fatalf("the reset left the old token valid: %v", err)
	}
	if _, err := f.service.Repository.Resolve(ctx, renewed); err != nil {
		t.Fatalf("new token: %v", err)
	}
	link, err = f.service.Repository.Link(ctx, f.u.ID)
	if err != nil || link != renewed {
		t.Fatalf("link after the reset: %q %v", link, err)
	}
}
