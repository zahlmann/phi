package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zahlmann/phi"
	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
)

func main() {
	authMode := provider.AuthMode(strings.TrimSpace(os.Getenv("PHI_AUTH_MODE")))
	if authMode == "" {
		authMode = provider.AuthModeOpenAIAPIKey
	}

	if authMode == provider.AuthModeOpenAIAPIKey && strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == "" {
		fmt.Println("Set OPENAI_API_KEY first (or PHI_AUTH_MODE=chatgpt).")
		os.Exit(1)
	}

	rt := phi.NewRuntime(phi.RuntimeOptions{
		AuthMode:     authMode,
		SystemPrompt: "You are a concise coding assistant.",
		WorkingDir:   ".",
	})
	defer rt.Close()

	final := make(chan phi.Event, 1)
	unsubscribe := rt.Subscribe(func(ev phi.Event) {
		switch ev.Type {
		case phi.EventToolCallStarted:
			fmt.Printf("[tool_started] %s (%s)\n", ev.ToolName, ev.ToolCallID)
		case phi.EventToolCallFinished:
			fmt.Printf("[tool_finished] %s (%s) error=%v\n", ev.ToolName, ev.ToolCallID, ev.IsError)
		case phi.EventFinalMessage:
			final <- ev
		}
	})
	defer unsubscribe()

	prompt := strings.Join([]string{
		"Use the bash tool only.",
		"Run: ls -1",
		"Then summarize what you observed in one sentence.",
	}, " ")

	started, err := rt.StartSession(context.Background(), phi.StartSessionRequest{
		Prompt: prompt,
	})
	if err != nil {
		fmt.Printf("start failed: %v\n", err)
		os.Exit(1)
	}

	select {
	case ev := <-final:
		if ev.SessionID != started.SessionID {
			fmt.Printf("session mismatch: got %s expected %s\n", ev.SessionID, started.SessionID)
			os.Exit(1)
		}
		if ev.AssistantMessage != nil {
			fmt.Printf("[assistant_final] %s\n", extractText(ev.AssistantMessage.ContentRaw))
		}
		if strings.TrimSpace(ev.Error) != "" {
			fmt.Printf("[assistant_error] %s\n", ev.Error)
		}
	case <-time.After(60 * time.Second):
		fmt.Println("timed out waiting for final message")
		os.Exit(1)
	}
	fmt.Println()
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
