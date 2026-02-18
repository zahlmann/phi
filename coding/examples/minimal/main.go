package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/zahlmann/phi/agent"
	openaiauth "github.com/zahlmann/phi/ai/auth/openai"
	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
	"github.com/zahlmann/phi/ai/stream"
	"github.com/zahlmann/phi/coding/sdk"
	"github.com/zahlmann/phi/coding/session"
	"github.com/zahlmann/phi/coding/tools"
)

func main() {
	authMode := provider.AuthMode(strings.TrimSpace(os.Getenv("PHI_AUTH_MODE")))
	if authMode == "" {
		authMode = provider.AuthModeOpenAIAPIKey
	}
	modelID := "gpt-5.2-codex"
	if authMode == provider.AuthModeChatGPT {
		modelID = "gpt-5.3-codex"
	}

	client := provider.NewOpenAIClient()
	manager := session.NewInMemoryManager("demo-session")
	toolset := tools.NewCodingTools(".")
	options := sdk.CreateSessionOptions{
		SystemPrompt:   "You are a concise coding assistant.",
		Model:          &model.Model{Provider: "openai", ID: modelID},
		ThinkingLevel:  agent.ThinkingHigh,
		Tools:          toolset,
		SessionManager: manager,
		ProviderClient: client,
		AuthMode:       authMode,
	}

	switch authMode {
	case provider.AuthModeChatGPT:
		authManager := openaiauth.NewDefaultManager()
		if strings.TrimSpace(os.Getenv("PHI_CHATGPT_LOGIN")) == "1" {
			if _, err := authManager.LoginInteractive(context.Background(), os.Stdin, os.Stdout); err != nil {
				fmt.Printf("chatgpt login failed: %v\n", err)
				os.Exit(1)
			}
		}

		creds, err := authManager.LoadOrRefresh(context.Background())
		if err != nil {
			fmt.Printf("failed to load chatgpt credentials: %v\n", err)
			os.Exit(1)
		}
		if creds == nil || strings.TrimSpace(creds.AccessToken) == "" {
			fmt.Println("No ChatGPT credentials found. Set PHI_CHATGPT_LOGIN=1 to run interactive login.")
			os.Exit(1)
		}
		options.AccessToken = creds.AccessToken
		options.AccountID = creds.AccountID
	default:
		apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		if apiKey == "" {
			fmt.Println("Set OPENAI_API_KEY first (or PHI_AUTH_MODE=chatgpt).")
			os.Exit(1)
		}
		options.APIKey = apiKey
	}

	s := sdk.CreateAgentSession(options)

	unsubscribe := s.Subscribe(func(ev agent.Event) {
		fmt.Printf("[event] %s", ev.Type)
		if ev.ToolName != "" {
			fmt.Printf(" tool=%s", ev.ToolName)
		}
		if ev.ToolCallID != "" {
			fmt.Printf(" call_id=%s", ev.ToolCallID)
		}
		if ev.IsError {
			fmt.Print(" error=true")
		}
		fmt.Println()

		if se, ok := ev.Message.(stream.Event); ok {
			switch se.Type {
			case stream.EventTextDelta:
				if se.Delta != "" {
					fmt.Printf("[text_delta] %s\n", se.Delta)
				}
			case stream.EventThinkingDelta:
				if se.Delta != "" {
					fmt.Printf("[thinking_delta] %s\n", se.Delta)
				}
			case stream.EventToolCall:
				fmt.Printf("[tool_call] name=%s id=%s args=%v\n", se.ToolName, se.ToolCallID, se.Arguments)
			case stream.EventDone:
				fmt.Printf("[stream_done] reason=%s\n", se.Reason)
			case stream.EventError:
				fmt.Printf("[stream_error] %s\n", se.Error)
			}
		}

		switch msg := ev.Message.(type) {
		case model.AssistantMessage:
			text := extractText(msg.ContentRaw)
			if text != "" {
				fmt.Printf("[assistant_final] %s\n", text)
			}
		case model.Message:
			if msg.Role == model.RoleToolResult {
				text := extractText(msg.ContentRaw)
				fmt.Printf("[tool_result] tool=%s id=%s output=%s\n", msg.ToolName, msg.ToolCallID, text)
			}
		}
	})
	defer unsubscribe()

	prompt := strings.Join([]string{
		"Use all available tools at least once in this order: write, read, edit, bash.",
		"1) write: create tmp_demo/main.go with a tiny Go program that prints 'hello from go'.",
		"2) read: read tmp_demo/main.go and briefly confirm what it currently prints.",
		"3) edit: change the output text to 'hello from edited go'.",
		"4) bash: run `go run ./tmp_demo` and report the output.",
		"End with a short summary of what changed and what command output you observed.",
	}, " ")

	if err := s.Prompt(prompt, sdk.PromptOptions{}); err != nil {
		fmt.Printf("prompt error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
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
