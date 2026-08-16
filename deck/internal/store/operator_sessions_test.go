package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmlero/the-platform/deck/internal/domain"
)

func TestOperatorSessionsPersistAndExpireWithoutRawCredentials(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "operator-sessions.db")
	appStore, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	expiresAt := time.Now().UTC().Add(time.Hour).UnixMilli()
	want := OperatorSession{
		TokenHash:  "88b9e84b7d92649767c6e77fe5f4f01da1f578aa9f8e502f212b23e4a70908bd",
		CSRFHash:   "73c47a56e4cc6fa117a45e38348d2b21f7dba056c659ad928e81ddf4d33c2a22",
		OperatorID: "operator-primary",
		ExpiresAt:  expiresAt,
	}
	if err := appStore.CreateOperatorSession(ctx, want); err != nil {
		t.Fatalf("create operator session: %v", err)
	}
	if err := appStore.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	appStore, err = Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() {
		if err := appStore.Close(); err != nil {
			t.Errorf("close reopened store: %v", err)
		}
	})

	got, err := appStore.GetOperatorSession(ctx, want.TokenHash, time.Now().UTC().UnixMilli())
	if err != nil {
		t.Fatalf("get operator session: %v", err)
	}
	if got.TokenHash != want.TokenHash || got.CSRFHash != want.CSRFHash ||
		got.OperatorID != want.OperatorID || got.ExpiresAt != want.ExpiresAt {
		t.Fatalf("operator session = %#v, want %#v", got, want)
	}

	if _, err := appStore.GetOperatorSession(ctx, want.TokenHash, expiresAt+1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired session error = %v, want not found", err)
	}
	if err := appStore.DeleteOperatorSession(ctx, want.TokenHash); err != nil {
		t.Fatalf("delete expired operator session: %v", err)
	}
}

func TestOperatorSessionRejectsInvalidDigests(t *testing.T) {
	appStore, err := Open(context.Background(), filepath.Join(t.TempDir(), "operator-sessions.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { appStore.Close() })

	err = appStore.CreateOperatorSession(context.Background(), OperatorSession{
		TokenHash: "raw-session-token", CSRFHash: "raw-csrf-token",
		OperatorID: "operator-primary", ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("invalid digest error = %v, want invalid input", err)
	}
}
