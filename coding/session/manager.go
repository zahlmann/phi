package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Manager interface {
	SessionID() string
	SessionFile() string
	AppendMessage(message any) (string, error)
	AppendModelChange(provider, modelID string) (string, error)
	AppendThinkingLevelChange(level string) (string, error)
	BuildContext() (messages []any, thinkingLevel string, modelProvider string, modelID string)
}

type InMemoryManager struct {
	sessionID string
	entries   []any
}

func NewInMemoryManager(sessionID string) *InMemoryManager {
	return &InMemoryManager{sessionID: sessionID}
}

func (m *InMemoryManager) SessionID() string {
	return m.sessionID
}

func (m *InMemoryManager) SessionFile() string {
	return ""
}

func (m *InMemoryManager) AppendMessage(message any) (string, error) {
	if message == nil {
		return "", errors.New("message is nil")
	}
	m.entries = append(m.entries, message)
	return "in-memory-entry", nil
}

func (m *InMemoryManager) AppendModelChange(provider, modelID string) (string, error) {
	m.entries = append(m.entries, ModelChangeEntry{
		ModelID:  modelID,
		Provider: provider,
	})
	return "in-memory-model-change", nil
}

func (m *InMemoryManager) AppendThinkingLevelChange(level string) (string, error) {
	m.entries = append(m.entries, ThinkingLevelChangeEntry{
		ThinkingLevel: level,
	})
	return "in-memory-thinking-change", nil
}

func (m *InMemoryManager) BuildContext() ([]any, string, string, string) {
	return append([]any{}, m.entries...), "off", "", ""
}

type FileManager struct {
	mu        sync.Mutex
	sessionID string
	filePath  string
	entries   []any
}

func NewFileManager(sessionID, filePath string) (*FileManager, error) {
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	if filePath == "" {
		return nil, errors.New("session file path is required")
	}

	mgr := &FileManager{
		sessionID: sessionID,
		filePath:  filePath,
		entries:   []any{},
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filePath)
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var raw map[string]any
			if err := json.Unmarshal([]byte(line), &raw); err == nil {
				mgr.entries = append(mgr.entries, raw)
			}
		}
	}
	return mgr, nil
}

func (m *FileManager) SessionID() string {
	return m.sessionID
}

func (m *FileManager) SessionFile() string {
	return m.filePath
}

func (m *FileManager) AppendMessage(message any) (string, error) {
	if message == nil {
		return "", errors.New("message is nil")
	}
	entryID := entryID("msg")
	entry := MessageEntry{
		EntryBase: newEntryBase("message", entryID),
		Message:   message,
	}
	return entryID, m.append(entry)
}

func (m *FileManager) AppendModelChange(provider, modelID string) (string, error) {
	entryID := entryID("model")
	entry := ModelChangeEntry{
		EntryBase: newEntryBase("model_change", entryID),
		Provider:  provider,
		ModelID:   modelID,
	}
	return entryID, m.append(entry)
}

func (m *FileManager) AppendThinkingLevelChange(level string) (string, error) {
	entryID := entryID("thinking")
	entry := ThinkingLevelChangeEntry{
		EntryBase:     newEntryBase("thinking_level_change", entryID),
		ThinkingLevel: level,
	}
	return entryID, m.append(entry)
}

func (m *FileManager) BuildContext() ([]any, string, string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]any{}, m.entries...)
	return out, "off", "", ""
}

func (m *FileManager) append(entry any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(m.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(payload, '\n')); err != nil {
		return err
	}
	m.entries = append(m.entries, entry)
	return nil
}

func entryID(prefix string) string {
	return prefix + "-" + time.Now().UTC().Format("20060102T150405.000000000")
}

func newEntryBase(kind, id string) EntryBase {
	return EntryBase{
		Type:      kind,
		ID:        id,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
}
