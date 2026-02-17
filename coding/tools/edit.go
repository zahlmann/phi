package tools

import (
	"fmt"
	"os"
	"strings"

	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/ai/model"
)

type editTool struct {
	cwd string
}

func NewEditTool(cwd string) agent.Tool {
	return &editTool{cwd: defaultCWD(cwd)}
}

func (t *editTool) Name() string {
	return "edit"
}

func (t *editTool) Description() string {
	return "Edit a file by replacing exact oldText with newText."
}

func (t *editTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Relative file path"},
			"oldText": map[string]any{
				"type":        "string",
				"description": "Exact text to replace",
			},
			"newText": map[string]any{
				"type":        "string",
				"description": "Replacement text",
			},
		},
		"required": []string{"path", "oldText", "newText"},
	}
}

func (t *editTool) Execute(toolCallID string, args map[string]any) (agent.ToolResult, error) {
	path, ok := toStringArg(args, "path")
	if !ok || strings.TrimSpace(path) == "" {
		return agent.ToolResult{}, fmt.Errorf("missing required argument: path")
	}
	oldText, ok := toStringArg(args, "oldText")
	if !ok {
		return agent.ToolResult{}, fmt.Errorf("missing required argument: oldText")
	}
	newText, ok := toStringArg(args, "newText")
	if !ok {
		return agent.ToolResult{}, fmt.Errorf("missing required argument: newText")
	}

	target, err := resolveSafePath(t.cwd, path)
	if err != nil {
		return agent.ToolResult{}, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return agent.ToolResult{}, err
	}
	content := string(data)

	matchCount := strings.Count(content, oldText)
	if matchCount == 0 {
		return agent.ToolResult{}, fmt.Errorf("could not find exact text in %s", path)
	}
	if matchCount > 1 {
		return agent.ToolResult{}, fmt.Errorf("oldText occurs multiple times in %s; provide unique context", path)
	}

	updated := strings.Replace(content, oldText, newText, 1)
	if updated == content {
		return agent.ToolResult{}, fmt.Errorf("no changes applied")
	}

	if err := os.WriteFile(target, []byte(updated), 0o644); err != nil {
		return agent.ToolResult{}, err
	}

	diff, firstChangedLine := generateDiffString(content, updated)
	return agent.ToolResult{
		Content: []model.TextContent{
			{
				Type: model.ContentText,
				Text: fmt.Sprintf("Edited %s: replaced %d chars with %d chars", path, len(oldText), len(newText)),
			},
		},
		Details: map[string]any{
			"path":             path,
			"diff":             diff,
			"firstChangedLine": firstChangedLine,
			"usedFuzzyMatch":   false,
		},
	}, nil
}

func generateDiffString(oldContent, newContent string) (string, int) {
	if oldContent == newContent {
		return "", 0
	}

	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")
	minLen := len(oldLines)
	if len(newLines) < minLen {
		minLen = len(newLines)
	}

	firstChangedLine := 1
	for i := 0; i < minLen; i++ {
		if oldLines[i] != newLines[i] {
			firstChangedLine = i + 1
			goto build
		}
	}
	firstChangedLine = minLen + 1

build:
	var out strings.Builder
	for i, line := range oldLines {
		fmt.Fprintf(&out, "-%d %s\n", i+1, line)
	}
	for i, line := range newLines {
		fmt.Fprintf(&out, "+%d %s\n", i+1, line)
	}
	return strings.TrimSuffix(out.String(), "\n"), firstChangedLine
}
