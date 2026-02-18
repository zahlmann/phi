package openai

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFileTokenStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	store := NewFileTokenStore(path)

	creds := &Credentials{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		AccountID:    "acc_123",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Round(time.Second),
	}
	if err := store.Save(context.Background(), creds); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected credentials")
	}
	if loaded.AccessToken != creds.AccessToken || loaded.AccountID != creds.AccountID {
		t.Fatalf("unexpected loaded credentials: %#v", loaded)
	}

	if err := store.Clear(context.Background()); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	loaded, err = store.Load(context.Background())
	if err != nil {
		t.Fatalf("load after clear failed: %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil credentials after clear, got %#v", loaded)
	}
}
