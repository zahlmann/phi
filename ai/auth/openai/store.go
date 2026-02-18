package openai

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type FileTokenStore struct {
	Path string
}

func NewFileTokenStore(path string) *FileTokenStore {
	return &FileTokenStore{Path: path}
}

func NewDefaultTokenStore() *FileTokenStore {
	return &FileTokenStore{}
}

func DefaultTokenStorePath() string {
	if override := strings.TrimSpace(os.Getenv("PHI_CHATGPT_TOKEN_PATH")); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".phi/chatgpt_tokens.json"
	}
	return filepath.Join(home, ".phi", "chatgpt_tokens.json")
}

func (s *FileTokenStore) Load(context.Context) (*Credentials, error) {
	path := s.resolvedPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	if strings.TrimSpace(creds.AccessToken) == "" {
		return nil, nil
	}
	return &creds, nil
}

func (s *FileTokenStore) Save(_ context.Context, credentials *Credentials) error {
	if credentials == nil {
		return errors.New("credentials are required")
	}
	path := s.resolvedPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(path, payload, 0o600)
}

func (s *FileTokenStore) Clear(context.Context) error {
	path := s.resolvedPath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *FileTokenStore) resolvedPath() string {
	if s != nil && strings.TrimSpace(s.Path) != "" {
		return s.Path
	}
	return DefaultTokenStorePath()
}
