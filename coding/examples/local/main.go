package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
	"github.com/zahlmann/phi/ai/stream"
	"github.com/zahlmann/phi/coding/sdk"
	"github.com/zahlmann/phi/coding/session"
	"github.com/zahlmann/phi/coding/tools"
)

func main() {
	repoRoot, err := repoRoot()
	if err != nil {
		fmt.Printf("failed to locate repo root: %v\n", err)
		os.Exit(1)
	}

	demoPath := "tmp_local_demo/main.go"
	_ = os.RemoveAll(filepath.Join(repoRoot, "tmp_local_demo"))

	client := provider.MockClient{Handler: deterministicHandler(demoPath)}
	s := sdk.CreateAgentSession(sdk.CreateSessionOptions{
		SystemPrompt:   "Run a deterministic local tool flow.",
		Model:          &model.Model{Provider: "mock", ID: "deterministic-local"},
		ThinkingLevel:  agent.ThinkingOff,
		Tools:          tools.NewCodingTools(repoRoot),
		SessionManager: session.NewInMemoryManager("local-demo"),
		ProviderClient: client,
	})

	unsubscribe := s.Subscribe(func(ev agent.Event) {
		if ev.ToolName != "" {
			fmt.Printf("[%s] tool=%s call_id=%s\n", ev.Type, ev.ToolName, ev.ToolCallID)
		}
		switch msg := ev.Message.(type) {
		case model.Message:
			if msg.Role == model.RoleToolResult {
				fmt.Printf("[tool_result] %s\n", extractText(msg.ContentRaw))
			}
		case model.AssistantMessage:
			text := extractText(msg.ContentRaw)
			if strings.TrimSpace(text) != "" {
				fmt.Printf("[assistant_final] %s\n", text)
			}
		}
	})
	defer unsubscribe()

	if err := s.Prompt("run local deterministic tool demo", sdk.PromptOptions{}); err != nil {
		fmt.Printf("prompt error: %v\n", err)
		os.Exit(1)
	}

	finalPath := filepath.Join(repoRoot, demoPath)
	data, err := os.ReadFile(finalPath)
	if err != nil {
		fmt.Printf("failed to read %s: %v\n", finalPath, err)
		os.Exit(1)
	}

	fmt.Printf("\nCreated: %s\n", finalPath)
	fmt.Printf("Final file contents:\n%s\n", string(data))
}

func deterministicHandler(path string) func(context.Context, model.Model, model.Context, provider.StreamOptions) (stream.EventStream, error) {
	return func(ctx context.Context, m model.Model, conversation model.Context, options provider.StreamOptions) (stream.EventStream, error) {
		switch toolResultCount(conversation.Messages) {
		case 0:
			return toolCallStream("call_write", "write", map[string]any{
				"path": path,
				"content": strings.Join([]string{
					"package main",
					"",
					"import \"fmt\"",
					"",
					"func main() {",
					"\tfmt.Println(\"hello from local go\")",
					"}",
					"",
				}, "\n"),
			}, m), nil
		case 1:
			return toolCallStream("call_read", "read", map[string]any{
				"path": path,
			}, m), nil
		case 2:
			return toolCallStream("call_edit", "edit", map[string]any{
				"path":    path,
				"oldText": "hello from local go",
				"newText": "hello from edited local go",
			}, m), nil
		case 3:
			return toolCallStream("call_bash", "bash", map[string]any{
				"command": "go run ./tmp_local_demo/main.go",
			}, m), nil
		default:
			return textStream("Local deterministic demo complete: write, read, edit, bash all executed.", m), nil
		}
	}
}

func toolResultCount(messages []model.Message) int {
	count := 0
	for _, message := range messages {
		if message.Role == model.RoleToolResult {
			count++
		}
	}
	return count
}

func toolCallStream(callID, name string, args map[string]any, m model.Model) stream.EventStream {
	return &stream.MockStream{
		Events: []stream.Event{
			{Type: stream.EventStart},
			{Type: stream.EventToolCall, ToolName: name, ToolCallID: callID, Arguments: args},
			{Type: stream.EventDone},
		},
		ResultValue: &model.AssistantMessage{
			Role: model.RoleAssistant,
			ContentRaw: []any{
				model.ToolCallContent{
					Type:      model.ContentToolCall,
					ID:        callID,
					Name:      name,
					Arguments: args,
				},
			},
			Provider:   m.Provider,
			Model:      m.ID,
			StopReason: model.StopReasonToolUse,
		},
	}
}

func textStream(text string, m model.Model) stream.EventStream {
	return &stream.MockStream{
		Events: []stream.Event{
			{Type: stream.EventStart},
			{Type: stream.EventTextDelta, Delta: text},
			{Type: stream.EventDone},
		},
		ResultValue: &model.AssistantMessage{
			Role:       model.RoleAssistant,
			ContentRaw: []any{model.TextContent{Type: model.ContentText, Text: text}},
			Provider:   m.Provider,
			Model:      m.ID,
			StopReason: model.StopReasonStop,
		},
	}
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	if _, err := os.Stat(root); err != nil {
		return "", err
	}
	return root, nil
}

func extractText(content []any) string {
	parts := []string{}
	for _, item := range content {
		switch v := item.(type) {
		case model.TextContent:
			if strings.TrimSpace(v.Text) != "" {
				parts = append(parts, v.Text)
			}
		case map[string]any:
			kind, _ := v["type"].(string)
			if kind == string(model.ContentText) {
				text, _ := v["text"].(string)
				if strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}
