package openai

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManagerLoadOrRefresh(t *testing.T) {
	t.Run("returns current credentials when still valid", func(t *testing.T) {
		store := &fakeTokenStore{
			loadValue: &Credentials{
				AccessToken:  "token",
				RefreshToken: "refresh",
				ExpiresAt:    time.Now().Add(2 * time.Minute),
			},
		}
		client := &fakeOAuthClient{}
		mgr := &Manager{Client: client, Store: store}

		creds, err := mgr.LoadOrRefresh(context.Background())
		if err != nil {
			t.Fatalf("LoadOrRefresh failed: %v", err)
		}
		if creds.AccessToken != "token" {
			t.Fatalf("unexpected credentials: %#v", creds)
		}
		if client.refreshCalls != 0 {
			t.Fatalf("expected no refresh call, got %d", client.refreshCalls)
		}
	})

	t.Run("refreshes expired credentials and saves", func(t *testing.T) {
		store := &fakeTokenStore{
			loadValue: &Credentials{
				AccessToken:  "old",
				RefreshToken: "refresh",
				ExpiresAt:    time.Now().Add(-time.Minute),
			},
		}
		client := &fakeOAuthClient{
			refreshValue: &Credentials{
				AccessToken:  "new",
				RefreshToken: "refresh2",
				ExpiresAt:    time.Now().Add(time.Minute),
			},
		}
		mgr := &Manager{Client: client, Store: store}

		creds, err := mgr.LoadOrRefresh(context.Background())
		if err != nil {
			t.Fatalf("LoadOrRefresh failed: %v", err)
		}
		if creds.AccessToken != "new" {
			t.Fatalf("expected refreshed token, got %#v", creds)
		}
		if client.refreshCalls != 1 {
			t.Fatalf("expected one refresh call, got %d", client.refreshCalls)
		}
		if store.saved == nil || store.saved.AccessToken != "new" {
			t.Fatalf("expected refreshed credentials to be saved, got %#v", store.saved)
		}
	})

	t.Run("propagates load error", func(t *testing.T) {
		mgr := &Manager{
			Client: &fakeOAuthClient{},
			Store:  &fakeTokenStore{loadErr: errors.New("load failed")},
		}
		_, err := mgr.LoadOrRefresh(context.Background())
		if err == nil || err.Error() != "load failed" {
			t.Fatalf("expected load error, got %v", err)
		}
	})

	t.Run("returns nil when store has no credentials", func(t *testing.T) {
		mgr := &Manager{
			Client: &fakeOAuthClient{},
			Store:  &fakeTokenStore{},
		}
		creds, err := mgr.LoadOrRefresh(context.Background())
		if err != nil {
			t.Fatalf("expected nil,nil for missing credentials, got err=%v", err)
		}
		if creds != nil {
			t.Fatalf("expected nil credentials, got %#v", creds)
		}
	})

	t.Run("propagates refresh and save errors", func(t *testing.T) {
		store := &fakeTokenStore{
			loadValue: &Credentials{
				AccessToken:  "old",
				RefreshToken: "refresh",
				ExpiresAt:    time.Now().Add(-time.Minute),
			},
		}
		client := &fakeOAuthClient{refreshErr: errors.New("refresh failed")}
		mgr := &Manager{Client: client, Store: store}
		_, err := mgr.LoadOrRefresh(context.Background())
		if err == nil || err.Error() != "refresh failed" {
			t.Fatalf("expected refresh error, got %v", err)
		}

		client = &fakeOAuthClient{
			refreshValue: &Credentials{
				AccessToken:  "new",
				RefreshToken: "refresh2",
				ExpiresAt:    time.Now().Add(time.Minute),
			},
		}
		store.saveErr = errors.New("save failed")
		mgr = &Manager{Client: client, Store: store}
		_, err = mgr.LoadOrRefresh(context.Background())
		if err == nil || err.Error() != "save failed" {
			t.Fatalf("expected save error, got %v", err)
		}
	})
}

type fakeTokenStore struct {
	loadValue *Credentials
	loadErr   error
	saved     *Credentials
	saveErr   error
}

func (f *fakeTokenStore) Load(context.Context) (*Credentials, error) {
	return f.loadValue, f.loadErr
}

func (f *fakeTokenStore) Save(_ context.Context, credentials *Credentials) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	if credentials != nil {
		copy := *credentials
		f.saved = &copy
	}
	return nil
}

func (f *fakeTokenStore) Clear(context.Context) error {
	return nil
}

type fakeOAuthClient struct {
	refreshCalls int
	refreshValue *Credentials
	refreshErr   error
}

func (f *fakeOAuthClient) StartDeviceFlow(context.Context) (*DeviceCode, error) {
	return nil, nil
}

func (f *fakeOAuthClient) PollDeviceFlow(context.Context, *DeviceCode) (*Credentials, error) {
	return nil, nil
}

func (f *fakeOAuthClient) Refresh(context.Context, string) (*Credentials, error) {
	f.refreshCalls++
	if f.refreshErr != nil {
		return nil, f.refreshErr
	}
	return f.refreshValue, nil
}
