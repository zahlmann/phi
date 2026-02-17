package tools

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/ai/model"
)

type bashTool struct {
	cwd     string
	timeout time.Duration
}

func NewBashTool(cwd string, timeout time.Duration) agent.Tool {
	return &bashTool{cwd: defaultCWD(cwd), timeout: timeout}
}

func (t *bashTool) Name() string {
	return "bash"
}

func (t *bashTool) Description() string {
	return "Execute a bash command in the working directory and return stdout/stderr."
}

func (t *bashTool) Parameters() map[string]any {
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

func (t *bashTool) Execute(toolCallID string, args map[string]any) (agent.ToolResult, error) {
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
