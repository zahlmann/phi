package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodingToolsContainMinimalSet(t *testing.T) {
	toolset := NewCodingTools(t.TempDir())
	names := map[string]bool{}
	for _, tool := range toolset {
		names[tool.Name()] = true
	}
	if len(toolset) != 4 {
		t.Fatalf("expected exactly 4 tools, got %d", len(toolset))
	}
	for _, required := range []string{"read", "write", "edit", "bash"} {
		if !names[required] {
			t.Fatalf("missing required tool: %s", required)
		}
	}
}

func TestWriteAndReadFileTools(t *testing.T) {
	dir := t.TempDir()
	writeTool := NewWriteFileTool(dir)
	readTool := NewReadFileTool(dir)

	writeResult, err := writeTool.Execute("c1", map[string]any{
		"path":    "hello.py",
		"content": "print('hello')\n",
	})
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if len(writeResult.Content) == 0 || !strings.Contains(writeResult.Content[0].Text, "Successfully wrote") {
		t.Fatalf("unexpected write output: %#v", writeResult.Content)
	}

	data, err := os.ReadFile(filepath.Join(dir, "hello.py"))
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	if string(data) != "print('hello')\n" {
		t.Fatalf("unexpected file content: %q", string(data))
	}

	result, err := readTool.Execute("c2", map[string]any{"path": "hello.py"})
	if err != nil {
		t.Fatalf("tool read failed: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected read content")
	}
	if !strings.Contains(result.Content[0].Text, "print('hello')") {
		t.Fatalf("unexpected tool output: %q", result.Content[0].Text)
	}
	if result.Details == nil || result.Details["path"] != "hello.py" {
		t.Fatalf("expected path detail, got %#v", result.Details)
	}
}

func TestWriteToolRejectsPathEscape(t *testing.T) {
	writeTool := NewWriteFileTool(t.TempDir())
	if _, err := writeTool.Execute("c4", map[string]any{
		"path":    "../escape.txt",
		"content": "bad",
	}); err == nil {
		t.Fatal("expected path escape error")
	}
}

func TestReadToolPagingAndBounds(t *testing.T) {
	dir := t.TempDir()
	writeTool := NewWriteFileTool(dir)
	readTool := NewReadFileTool(dir)

	content := "line1\nline2\nline3\nline4\nline5\n"
	if _, err := writeTool.Execute("w2", map[string]any{
		"path":    "notes.txt",
		"content": content,
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	result, err := readTool.Execute("r2", map[string]any{
		"path":   "notes.txt",
		"offset": 2,
		"limit":  2,
	})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "line2\nline3") {
		t.Fatalf("unexpected paged read output: %q", text)
	}
	if !strings.Contains(text, "Use offset=4 to continue") {
		t.Fatalf("missing continuation hint: %q", text)
	}

	_, err = readTool.Execute("r3", map[string]any{
		"path":   "notes.txt",
		"offset": 99,
	})
	if err == nil || !strings.Contains(err.Error(), "beyond end of file") {
		t.Fatalf("expected offset bounds error, got %v", err)
	}
}

func TestReadToolImagePayload(t *testing.T) {
	dir := t.TempDir()
	readTool := NewReadFileTool(dir)
	imgPath := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(imgPath, []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatalf("write image failed: %v", err)
	}

	result, err := readTool.Execute("img", map[string]any{"path": "photo.png"})
	if err != nil {
		t.Fatalf("read image failed: %v", err)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "Read image file [image/png]") {
		t.Fatalf("unexpected image output: %#v", result.Content)
	}
	if result.Details["mimeType"] != "image/png" {
		t.Fatalf("unexpected mime type details: %#v", result.Details)
	}
	image, ok := result.Details["image"].(map[string]any)
	if !ok || strings.TrimSpace(image["data"].(string)) == "" {
		t.Fatalf("expected base64 image details, got %#v", result.Details["image"])
	}
}

func TestReadToolFirstLineExceedsMaxBytes(t *testing.T) {
	dir := t.TempDir()
	writeTool := NewWriteFileTool(dir)
	readTool := NewReadFileTool(dir)
	if _, err := writeTool.Execute("w", map[string]any{
		"path":    "big.txt",
		"content": strings.Repeat("x", 40),
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	result, err := readTool.Execute("r", map[string]any{
		"path":      "big.txt",
		"max_bytes": 8,
	})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "exceeds 8B limit") {
		t.Fatalf("expected max-bytes warning, got %q", result.Content[0].Text)
	}
}

func TestEditTool(t *testing.T) {
	dir := t.TempDir()
	writeTool := NewWriteFileTool(dir)
	editTool := NewEditTool(dir)
	readTool := NewReadFileTool(dir)

	if _, err := writeTool.Execute("w1", map[string]any{
		"path":    "main.py",
		"content": "print('helo world')\n",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	result, err := editTool.Execute("e1", map[string]any{
		"path":    "main.py",
		"oldText": "helo world",
		"newText": "hello world",
	})
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected edit output")
	}
	if result.Details == nil || result.Details["diff"] == nil {
		t.Fatalf("expected diff in details, got %#v", result.Details)
	}

	readResult, err := readTool.Execute("r1", map[string]any{"path": "main.py"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(readResult.Content[0].Text, "hello world") {
		t.Fatalf("unexpected content after edit: %q", readResult.Content[0].Text)
	}
}

func TestEditToolValidation(t *testing.T) {
	dir := t.TempDir()
	writeTool := NewWriteFileTool(dir)
	editTool := NewEditTool(dir)

	if _, err := writeTool.Execute("w", map[string]any{
		"path":    "dupe.txt",
		"content": "same\nsame\n",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, err := editTool.Execute("e", map[string]any{
		"path":    "dupe.txt",
		"oldText": "same",
		"newText": "new",
	})
	if err == nil || !strings.Contains(err.Error(), "multiple times") {
		t.Fatalf("expected duplicate oldText error, got %v", err)
	}

	_, err = editTool.Execute("e2", map[string]any{
		"path":    "dupe.txt",
		"oldText": "missing",
		"newText": "new",
	})
	if err == nil || !strings.Contains(err.Error(), "could not find exact text") {
		t.Fatalf("expected missing oldText error, got %v", err)
	}
}

func TestBashTool(t *testing.T) {
	dir := t.TempDir()
	bashTool := NewBashTool(dir, 5*time.Second)
	result, err := bashTool.Execute("c3", map[string]any{"command": "echo test-output"})
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
	bashTool := NewBashTool(t.TempDir(), 5*time.Second)
	result, err := bashTool.Execute("b", map[string]any{
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
	bashTool := NewBashTool(t.TempDir(), 0)
	_, err := bashTool.Execute("timeout", map[string]any{
		"command": "sleep 1",
		"timeout": 0.05,
	})
	if err == nil || !strings.Contains(err.Error(), "command timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestBashToolTruncationSavesFullOutput(t *testing.T) {
	dir := t.TempDir()
	bashTool := NewBashTool(dir, 5*time.Second)

	result, err := bashTool.Execute("b1", map[string]any{
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
