package tools

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/ai/model"
)

type readFileTool struct {
	cwd string
}

func NewReadFileTool(cwd string) agent.Tool {
	return &readFileTool{cwd: defaultCWD(cwd)}
}

func (t *readFileTool) Name() string {
	return "read"
}

func (t *readFileTool) Description() string {
	return "Read a file path relative to the working directory."
}

func (t *readFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Relative file path"},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Line number to start reading from (1-indexed)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of lines to read",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": "Optional maximum bytes to return",
			},
		},
		"required": []string{"path"},
	}
}

func (t *readFileTool) Execute(toolCallID string, args map[string]any) (agent.ToolResult, error) {
	path, ok := toStringArg(args, "path")
	if !ok || strings.TrimSpace(path) == "" {
		return agent.ToolResult{}, fmt.Errorf("missing required argument: path")
	}

	target, err := resolveSafePath(t.cwd, path)
	if err != nil {
		return agent.ToolResult{}, err
	}

	if mimeType := detectImageMimeType(target); mimeType != "" {
		data, err := os.ReadFile(target)
		if err != nil {
			return agent.ToolResult{}, err
		}
		return agent.ToolResult{
			Content: []model.TextContent{
				{
					Type: model.ContentText,
					Text: fmt.Sprintf("Read image file [%s]", mimeType),
				},
			},
			Details: map[string]any{
				"path":     path,
				"mimeType": mimeType,
				"image": map[string]any{
					"type":     string(model.ContentImage),
					"mimeType": mimeType,
					"data":     base64.StdEncoding.EncodeToString(data),
				},
			},
		}, nil
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return agent.ToolResult{}, err
	}

	maxBytes := defaultMaxBytes
	if raw, ok := args["max_bytes"]; ok {
		if n, ok := toInt(raw); ok && n > 0 {
			maxBytes = n
		}
	}
	offset := 1
	if raw, ok := args["offset"]; ok {
		if n, ok := toInt(raw); ok && n > 0 {
			offset = n
		}
	}
	limit := 0
	if raw, ok := args["limit"]; ok {
		if n, ok := toInt(raw); ok && n > 0 {
			limit = n
		}
	}

	textContent := strings.ReplaceAll(string(data), "\r\n", "\n")
	textContent = strings.ReplaceAll(textContent, "\r", "\n")
	allLines := strings.Split(textContent, "\n")
	totalFileLines := len(allLines)
	startLine := maxInt(offset, 1)
	if startLine > totalFileLines {
		return agent.ToolResult{}, fmt.Errorf("offset %d is beyond end of file (%d lines total)", offset, totalFileLines)
	}

	startIdx := startLine - 1
	selected := allLines[startIdx:]
	userLimitedLines := 0
	if limit > 0 {
		endIdx := minInt(startIdx+limit, len(allLines))
		selected = allLines[startIdx:endIdx]
		userLimitedLines = endIdx - startIdx
	}
	selectedContent := strings.Join(selected, "\n")

	trunc := truncateHead(selectedContent, defaultMaxLines, maxBytes)
	outputText := trunc.Content
	details := map[string]any{"path": path}

	switch {
	case trunc.FirstLineExceedsLimit:
		outputText = fmt.Sprintf(
			"[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			startLine,
			formatSize(byteLen(allLines[startIdx])),
			formatSize(maxBytes),
			startLine,
			path,
			maxBytes,
		)
		details["truncation"] = trunc.toMap()
	case trunc.Truncated:
		endLine := startLine + trunc.OutputLines - 1
		nextOffset := endLine + 1
		if trunc.TruncatedBy == "lines" {
			outputText += fmt.Sprintf(
				"\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]",
				startLine, endLine, totalFileLines, nextOffset,
			)
		} else {
			outputText += fmt.Sprintf(
				"\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]",
				startLine, endLine, totalFileLines, formatSize(maxBytes), nextOffset,
			)
		}
		details["truncation"] = trunc.toMap()
	case userLimitedLines > 0 && startIdx+userLimitedLines < len(allLines):
		remaining := len(allLines) - (startIdx + userLimitedLines)
		nextOffset := startLine + userLimitedLines
		outputText += fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]", remaining, nextOffset)
	}

	if strings.TrimSpace(outputText) == "" {
		outputText = "(empty file)"
	}

	return agent.ToolResult{
		Content: []model.TextContent{
			{
				Type: model.ContentText,
				Text: outputText,
			},
		},
		Details: details,
	}, nil
}

func detectImageMimeType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}
