package bash

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestToolHasBashName(t *testing.T) {
	tool := NewTool(t.TempDir(), 0)
	if tool.Name() != "bash" {
		t.Fatalf("expected tool name bash, got %q", tool.Name())
	}
}

func TestBashTool(t *testing.T) {
	dir := t.TempDir()
	tool := NewTool(dir, 5*time.Second)
	result, err := tool.Execute("c3", map[string]any{"command": "echo test-output"})
	if err != nil {
		t.Fatalf("bash failed: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected bash output")
	}
	if strings.TrimSpace(result.Content[0].Text) != "test-output" {
		t.Fatalf("unexpected bash output: %q", result.Content[0].Text)
	}
}

func TestBashToolReturnsExitCodeError(t *testing.T) {
	tool := NewTool(t.TempDir(), 5*time.Second)
	result, err := tool.Execute("b", map[string]any{
		"command": "echo boom && exit 7",
	})
	if err == nil || !strings.Contains(err.Error(), "Command exited with code 7") {
		t.Fatalf("expected exit code error, got %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "boom") {
		t.Fatalf("expected command output in result, got %q", result.Content[0].Text)
	}
}

func TestBashToolTimeout(t *testing.T) {
	tool := NewTool(t.TempDir(), 0)
	_, err := tool.Execute("timeout", map[string]any{
		"command": "sleep 1",
		"timeout": 0.05,
	})
	if err == nil || !strings.Contains(err.Error(), "command timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestBashToolTruncationSavesFullOutput(t *testing.T) {
	dir := t.TempDir()
	tool := NewTool(dir, 5*time.Second)

	result, err := tool.Execute("b1", map[string]any{
		"command": "for i in $(seq 1 3000); do echo \"$i\"; done",
	})
	if err != nil {
		t.Fatalf("bash failed: %v", err)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Showing lines") {
		t.Fatalf("expected truncation notice, got: %q", text)
	}

	fullPath, _ := result.Details["fullOutputPath"].(string)
	if strings.TrimSpace(fullPath) == "" {
		t.Fatalf("expected fullOutputPath in details: %#v", result.Details)
	}
	if _, err := os.Stat(fullPath); err != nil {
		t.Fatalf("expected full output file to exist at %s: %v", fullPath, err)
	}
}
