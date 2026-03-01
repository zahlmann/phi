package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zahlmann/phi"
	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
	"github.com/zahlmann/phi/ai/stream"
)

func main() {
	repoRoot, err := repoRoot()
	if err != nil {
		fmt.Printf("failed to locate repo root: %v\n", err)
		os.Exit(1)
	}

	demoPath := filepath.Join(repoRoot, "tmp_local_demo.txt")
	_ = os.Remove(demoPath)

	client := provider.MockClient{
		Handler: func(ctx context.Context, m model.Model, conversation model.Context, options provider.StreamOptions) (stream.EventStream, error) {
			if !conversationHasToolResult(conversation.Messages) {
				return toolCallStream("call_1", "bash", map[string]any{
					"command": "printf 'local demo from bash tool\\n' > tmp_local_demo.txt && cat tmp_local_demo.txt",
				}, m), nil
			}
			return textStream("Local deterministic demo complete using bash-only tooling.", m), nil
		},
	}

	rt := phi.NewRuntime(phi.RuntimeOptions{
		ProviderClient: client,
		SystemPrompt:   "Run a deterministic local tool flow with bash only.",
		WorkingDir:     repoRoot,
		ModelID:        "deterministic-local",
	})
	defer rt.Close()

	final := make(chan phi.Event, 1)
	unsubscribe := rt.Subscribe(func(ev phi.Event) {
		switch ev.Type {
		case phi.EventToolCallStarted:
			fmt.Printf("[tool_started] %s (%s)\n", ev.ToolName, ev.ToolCallID)
		case phi.EventToolCallFinished:
			fmt.Printf("[tool_finished] %s (%s) error=%v\n", ev.ToolName, ev.ToolCallID, ev.IsError)
			if ev.ToolResult != nil {
				fmt.Printf("[tool_output] %s\n", extractText(ev.ToolResult.ContentRaw))
			}
		case phi.EventFinalMessage:
			final <- ev
		}
	})
	defer unsubscribe()

	if _, err := rt.StartSession(context.Background(), phi.StartSessionRequest{
		Prompt: "run local deterministic bash demo",
	}); err != nil {
		fmt.Printf("start failed: %v\n", err)
		os.Exit(1)
	}

	select {
	case ev := <-final:
		if ev.AssistantMessage != nil {
			fmt.Printf("[assistant_final] %s\n", extractText(ev.AssistantMessage.ContentRaw))
		}
	case <-time.After(10 * time.Second):
		fmt.Println("timed out waiting for final response")
		os.Exit(1)
	}

	data, err := os.ReadFile(demoPath)
	if err != nil {
		fmt.Printf("failed to read %s: %v\n", demoPath, err)
		os.Exit(1)
	}
	fmt.Printf("\nCreated: %s\n", demoPath)
	fmt.Printf("Final file contents:\n%s\n", string(data))
}

func conversationHasToolResult(messages []model.Message) bool {
	for _, message := range messages {
		if message.Role == model.RoleToolResult {
			return true
		}
	}
	return false
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
		if text, ok := item.(model.TextContent); ok && strings.TrimSpace(text.Text) != "" {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}
