package bash

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/ai/model"
)

const (
	defaultMaxLines = 2000
	defaultMaxBytes = 50 * 1024
)

type Tool struct {
	cwd     string
	timeout time.Duration
}

func NewTool(cwd string, timeout time.Duration) agent.Tool {
	return &Tool{cwd: defaultCWD(cwd), timeout: timeout}
}

func (t *Tool) Name() string {
	return "bash"
}

func (t *Tool) Description() string {
	return "Execute a bash command in the working directory and return stdout/stderr."
}

func (t *Tool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Bash command to execute",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Timeout in seconds (optional, no default timeout)",
			},
		},
		"required": []string{"command"},
	}
}

func (t *Tool) Execute(toolCallID string, args map[string]any) (agent.ToolResult, error) {
	command, ok := toStringArg(args, "command")
	if !ok || strings.TrimSpace(command) == "" {
		return agent.ToolResult{}, fmt.Errorf("missing required argument: command")
	}

	timeout := t.timeout
	if raw, ok := args["timeout"]; ok {
		if secs, ok := toFloat(raw); ok && secs > 0 {
			timeout = time.Duration(secs * float64(time.Second))
		}
	}
	ctx := context.Background()
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Dir = t.cwd
	output, err := cmd.CombinedOutput()

	fullOutput := strings.ReplaceAll(string(output), "\r\n", "\n")
	fullOutput = strings.ReplaceAll(fullOutput, "\r", "\n")
	trunc := truncateTail(fullOutput, defaultMaxLines, defaultMaxBytes)
	outputText := trunc.Content
	if strings.TrimSpace(outputText) == "" {
		outputText = "(no output)"
	}

	var fullOutputPath string
	if trunc.Truncated {
		fullOutputPath = tempOutputFilePath("phi-bash")
		_ = os.WriteFile(fullOutputPath, []byte(fullOutput), 0o600)

		startLine := trunc.TotalLines - trunc.OutputLines + 1
		endLine := trunc.TotalLines
		if trunc.LastLinePartial {
			lastLineSize := formatSize(byteLen(lastLine(fullOutput)))
			outputText += fmt.Sprintf(
				"\n\n[Showing last %s of line %d (line is %s). Full output: %s]",
				formatSize(trunc.OutputBytes),
				endLine,
				lastLineSize,
				fullOutputPath,
			)
		} else if trunc.TruncatedBy == "lines" {
			outputText += fmt.Sprintf(
				"\n\n[Showing lines %d-%d of %d. Full output: %s]",
				startLine, endLine, trunc.TotalLines, fullOutputPath,
			)
		} else {
			outputText += fmt.Sprintf(
				"\n\n[Showing lines %d-%d of %d (%s limit). Full output: %s]",
				startLine, endLine, trunc.TotalLines, formatSize(defaultMaxBytes), fullOutputPath,
			)
		}
	}

	if ctx.Err() == context.DeadlineExceeded {
		outputText += fmt.Sprintf("\n\nCommand timed out after %.1f seconds", timeout.Seconds())
		err = fmt.Errorf("command timed out")
	}

	result := agent.ToolResult{
		Content: []model.TextContent{
			{Type: model.ContentText, Text: outputText},
		},
		Details: map[string]any{
			"command": command,
			"cwd":     t.cwd,
			"truncation": func() any {
				if trunc.Truncated {
					return trunc.toMap()
				}
				return nil
			}(),
			"fullOutputPath": fullOutputPath,
		},
	}
	if exitCode := exitCodeOf(err); exitCode != 0 && ctx.Err() == nil {
		return result, fmt.Errorf("%s\n\nCommand exited with code %d", outputText, exitCode)
	}
	return result, err
}

func defaultCWD(cwd string) string {
	if strings.TrimSpace(cwd) != "" {
		return cwd
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

func toStringArg(args map[string]any, key string) (string, bool) {
	raw, ok := args[key]
	if !ok {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return v, true
	case fmt.Stringer:
		return v.String(), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case int:
		return strconv.Itoa(v), true
	default:
		return fmt.Sprintf("%v", raw), true
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		parsed, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func tempOutputFilePath(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d.log", prefix, time.Now().UnixNano()))
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%x.log", prefix, buf))
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 0
}

type truncationResult struct {
	Content               string
	Truncated             bool
	TruncatedBy           string
	TotalLines            int
	TotalBytes            int
	OutputLines           int
	OutputBytes           int
	LastLinePartial       bool
	FirstLineExceedsLimit bool
	MaxLines              int
	MaxBytes              int
}

func (t truncationResult) toMap() map[string]any {
	return map[string]any{
		"truncated":             t.Truncated,
		"truncatedBy":           t.TruncatedBy,
		"totalLines":            t.TotalLines,
		"totalBytes":            t.TotalBytes,
		"outputLines":           t.OutputLines,
		"outputBytes":           t.OutputBytes,
		"lastLinePartial":       t.LastLinePartial,
		"firstLineExceedsLimit": t.FirstLineExceedsLimit,
		"maxLines":              t.MaxLines,
		"maxBytes":              t.MaxBytes,
	}
}

func truncateTail(content string, maxLines, maxBytes int) truncationResult {
	totalBytes := byteLen(content)
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	if totalLines <= maxLines && totalBytes <= maxBytes {
		return truncationResult{
			Content:     content,
			Truncated:   false,
			TruncatedBy: "",
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			OutputLines: totalLines,
			OutputBytes: totalBytes,
			MaxLines:    maxLines,
			MaxBytes:    maxBytes,
		}
	}

	out := []string{}
	outBytes := 0
	truncatedBy := "lines"
	lastLinePartial := false

	for i := len(lines) - 1; i >= 0 && len(out) < maxLines; i-- {
		line := lines[i]
		lineBytes := byteLen(line)
		if len(out) > 0 {
			lineBytes++
		}
		if outBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			if len(out) == 0 {
				truncated := truncateStringToBytesFromEnd(line, maxBytes)
				out = append([]string{truncated}, out...)
				outBytes = byteLen(truncated)
				lastLinePartial = true
			}
			break
		}
		out = append([]string{line}, out...)
		outBytes += lineBytes
	}
	if len(out) >= maxLines && outBytes <= maxBytes {
		truncatedBy = "lines"
	}

	outContent := strings.Join(out, "\n")
	return truncationResult{
		Content:         outContent,
		Truncated:       true,
		TruncatedBy:     truncatedBy,
		TotalLines:      totalLines,
		TotalBytes:      totalBytes,
		OutputLines:     len(out),
		OutputBytes:     byteLen(outContent),
		LastLinePartial: lastLinePartial,
		MaxLines:        maxLines,
		MaxBytes:        maxBytes,
	}
}

func truncateStringToBytesFromEnd(s string, maxBytes int) string {
	raw := []byte(s)
	if len(raw) <= maxBytes {
		return s
	}
	start := len(raw) - maxBytes
	for start < len(raw) && (raw[start]&0xC0) == 0x80 {
		start++
	}
	return string(raw[start:])
}

func byteLen(s string) int {
	return len([]byte(s))
}

func formatSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024.0)
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/(1024.0*1024.0))
}
