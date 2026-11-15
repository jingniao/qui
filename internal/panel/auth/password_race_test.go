package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
)

type loginPasswordChangeSettings struct {
	auth.SetupSettings
	change func()
}

func (s loginPasswordChangeSettings) SessionLifetime(context.Context) (time.Duration, error) {
	s.change()
	return time.Hour, nil
}

func TestLoginRejectsPasswordChangedAfterVerification(t *testing.T) {
	t.Parallel()
	service, mux := fixture(t)
	_, owner := setup(t, mux)
	ctx := context.Background()
	service.Settings = loginPasswordChangeSettings{SetupSettings: service.Settings, change: func() {
		if err := service.ChangePassword(ctx, owner.CurrentUser, owner.CurrentUser.ID, testPassword, "replacement-password-123", "local", false); err != nil {
			t.Fatal(err)
		}
	}}
	token, _, err := service.Login(ctx, "owner", testPassword, "local")
	if !errors.Is(err, auth.ErrCredentials) || token != "" {
		t.Fatalf("old password left a session: token present=%v, error=%v", token != "", err)
	}
	var sessions int
	if err := service.Repository.Database.QueryRowContext(ctx, "SELECT count(*) FROM sessions").Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("password change left %d sessions", sessions)
	}
}

func TestConcurrentPasswordChangesUseVerifiedHash(t *testing.T) {
	t.Parallel()
	service, mux := fixture(t)
	_, owner := setup(t, mux)
	ctx := context.Background()
	var verifiedHash string
	if err := service.Repository.Database.QueryRowContext(ctx, "SELECT password_hash FROM admins WHERE id=?", owner.CurrentUser.ID).Scan(&verifiedHash); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, password := range []string{"first-replacement-123", "second-replacement-123"} {
		hash, err := auth.HashPassword(password)
		if err != nil {
			t.Fatal(err)
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- service.Repository.ChangePassword(ctx, owner.CurrentUser.ID, hash, verifiedHash)
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, auth.ErrConflict):
			conflicted++
		default:
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("got %d successful changes and %d conflicts, want one each", succeeded, conflicted)
	}
}
