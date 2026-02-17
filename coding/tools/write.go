package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/ai/model"
)

type writeFileTool struct {
	cwd string
}

func NewWriteFileTool(cwd string) agent.Tool {
	return &writeFileTool{cwd: defaultCWD(cwd)}
}

func (t *writeFileTool) Name() string {
	return "write"
}

func (t *writeFileTool) Description() string {
	return "Write content to a file. Creates the file if it doesn't exist, overwrites if it does, and creates parent directories automatically."
}

func (t *writeFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to write (relative or absolute within the working directory)",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "File content to write",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *writeFileTool) Execute(toolCallID string, args map[string]any) (agent.ToolResult, error) {
	path, ok := toStringArg(args, "path")
	if !ok || strings.TrimSpace(path) == "" {
		return agent.ToolResult{}, fmt.Errorf("missing required argument: path")
	}
	content, ok := toStringArg(args, "content")
	if !ok {
		return agent.ToolResult{}, fmt.Errorf("missing required argument: content")
	}

	target, err := resolveSafePath(t.cwd, path)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return agent.ToolResult{}, err
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return agent.ToolResult{}, err
	}

	return agent.ToolResult{
		Content: []model.TextContent{
			{
				Type: model.ContentText,
				Text: fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path),
			},
		},
		Details: map[string]any{
			"path": path,
			"size": len(content),
		},
	}, nil
}
