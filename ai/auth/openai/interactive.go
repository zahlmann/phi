package openai

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

func (m *Manager) LoginInteractive(ctx context.Context, in io.Reader, out io.Writer) (*Credentials, error) {
	if m == nil || m.Client == nil || m.Store == nil {
		return nil, errors.New("manager client and store are required")
	}
	if in == nil {
		in = strings.NewReader("\n")
	}
	if out == nil {
		out = io.Discard
	}

	device, err := m.Client.StartDeviceFlow(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(out, "Authenticate with ChatGPT:\n%s\n", device.VerificationURI)
	fmt.Fprintf(out, "Enter code: %s\n", device.UserCode)
	fmt.Fprint(out, "Paste access token (optional, press Enter to auto-complete login): ")

	reader := bufio.NewReader(in)
	line, readErr := reader.ReadString('\n')
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, readErr
	}
	manualToken := strings.TrimSpace(line)

	if manualToken != "" {
		creds := &Credentials{
			AccessToken: manualToken,
			AccountID:   extractAccountIDFromJWT(manualToken),
			ExpiresAt:   time.Now().Add(55 * time.Minute),
		}
		fmt.Fprintf(out, "Account ID (optional, press Enter to keep %q): ", creds.AccountID)
		accountLine, accountErr := reader.ReadString('\n')
		if accountErr != nil && !errors.Is(accountErr, io.EOF) {
			return nil, accountErr
		}
		if accountID := strings.TrimSpace(accountLine); accountID != "" {
			creds.AccountID = accountID
		}
		if err := m.Store.Save(ctx, creds); err != nil {
			return nil, err
		}
		fmt.Fprintln(out, "Saved ChatGPT token.")
		return creds, nil
	}

	creds, err := m.Client.PollDeviceFlow(ctx, device)
	if err != nil {
		return nil, err
	}
	if err := m.Store.Save(ctx, creds); err != nil {
		return nil, err
	}
	fmt.Fprintln(out, "ChatGPT login complete and token saved.")
	return creds, nil
}
