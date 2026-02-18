package openai

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestManagerLoginInteractiveManualToken(t *testing.T) {
	store := &fakeTokenStore{}
	client := &interactiveFakeClient{
		startCode: &DeviceCode{
			VerificationURI: "https://auth.openai.com/codex/device",
			UserCode:        "ABC-123",
		},
	}
	mgr := &Manager{Client: client, Store: store}

	in := strings.NewReader("manual-token\nacc_manual\n")
	out := &bytes.Buffer{}

	creds, err := mgr.LoginInteractive(context.Background(), in, out)
	if err != nil {
		t.Fatalf("LoginInteractive failed: %v", err)
	}
	if creds.AccessToken != "manual-token" {
		t.Fatalf("unexpected creds: %#v", creds)
	}
	if creds.AccountID != "acc_manual" {
		t.Fatalf("unexpected account id: %#v", creds)
	}
	if client.pollCalls != 0 {
		t.Fatalf("expected no poll call, got %d", client.pollCalls)
	}
	if store.saved == nil || store.saved.AccessToken != "manual-token" {
		t.Fatalf("expected saved manual token, got %#v", store.saved)
	}
}

func TestManagerLoginInteractiveDeviceFlow(t *testing.T) {
	store := &fakeTokenStore{}
	client := &interactiveFakeClient{
		startCode: &DeviceCode{
			VerificationURI: "https://auth.openai.com/codex/device",
			UserCode:        "XYZ-789",
		},
		pollValue: &Credentials{
			AccessToken:  "device-token",
			RefreshToken: "refresh",
			AccountID:    "acc_device",
			ExpiresAt:    time.Now().Add(time.Hour),
		},
	}
	mgr := &Manager{Client: client, Store: store}

	creds, err := mgr.LoginInteractive(context.Background(), strings.NewReader("\n"), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("LoginInteractive failed: %v", err)
	}
	if creds.AccessToken != "device-token" {
		t.Fatalf("unexpected creds: %#v", creds)
	}
	if client.pollCalls != 1 {
		t.Fatalf("expected one poll call, got %d", client.pollCalls)
	}
	if store.saved == nil || store.saved.AccessToken != "device-token" {
		t.Fatalf("expected saved device token, got %#v", store.saved)
	}
}

type interactiveFakeClient struct {
	startCode *DeviceCode
	pollValue *Credentials
	pollCalls int
}

func (f *interactiveFakeClient) StartDeviceFlow(context.Context) (*DeviceCode, error) {
	return f.startCode, nil
}

func (f *interactiveFakeClient) PollDeviceFlow(context.Context, *DeviceCode) (*Credentials, error) {
	f.pollCalls++
	return f.pollValue, nil
}

func (f *interactiveFakeClient) Refresh(context.Context, string) (*Credentials, error) {
	return nil, nil
}
