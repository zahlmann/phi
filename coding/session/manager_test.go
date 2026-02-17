package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInMemoryManager(t *testing.T) {
	mgr := NewInMemoryManager("s1")
	if _, err := mgr.AppendMessage(map[string]any{"role": "user"}); err != nil {
		t.Fatalf("append message failed: %v", err)
	}
	if _, err := mgr.AppendModelChange("openai", "gpt-test"); err != nil {
		t.Fatalf("append model change failed: %v", err)
	}
	if _, err := mgr.AppendThinkingLevelChange("low"); err != nil {
		t.Fatalf("append thinking change failed: %v", err)
	}

	entries, thinking, provider, modelID := mgr.BuildContext()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if thinking != "off" || provider != "" || modelID != "" {
		t.Fatalf("unexpected defaults from BuildContext: thinking=%q provider=%q modelID=%q", thinking, provider, modelID)
	}
}

func TestInMemoryManagerRejectsNilMessage(t *testing.T) {
	mgr := NewInMemoryManager("s1")
	_, err := mgr.AppendMessage(nil)
	if err == nil || !strings.Contains(err.Error(), "message is nil") {
		t.Fatalf("expected nil message error, got %v", err)
	}
}

func TestFileManagerValidation(t *testing.T) {
	_, err := NewFileManager("", "sessions/s1.jsonl")
	if err == nil || !strings.Contains(err.Error(), "session id is required") {
		t.Fatalf("expected session id validation error, got %v", err)
	}

	_, err = NewFileManager("s1", "")
	if err == nil || !strings.Contains(err.Error(), "session file path is required") {
		t.Fatalf("expected path validation error, got %v", err)
	}
}

func TestFileManagerAppendAndReload(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sessions", "s1.jsonl")

	mgr, err := NewFileManager("s1", file)
	if err != nil {
		t.Fatalf("new file manager failed: %v", err)
	}
	if _, err := mgr.AppendMessage(map[string]any{"role": "user", "content": "hello"}); err != nil {
		t.Fatalf("append message failed: %v", err)
	}
	if _, err := mgr.AppendModelChange("openai", "gpt-test"); err != nil {
		t.Fatalf("append model failed: %v", err)
	}
	if _, err := mgr.AppendThinkingLevelChange("low"); err != nil {
		t.Fatalf("append thinking failed: %v", err)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected session file content")
	}

	mgr2, err := NewFileManager("s1", file)
	if err != nil {
		t.Fatalf("reload manager failed: %v", err)
	}
	entries, _, _, _ := mgr2.BuildContext()
	if len(entries) < 3 {
		t.Fatalf("expected at least 3 entries, got %d", len(entries))
	}
}
