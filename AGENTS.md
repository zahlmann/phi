# phi usage guide

This repo is the core `phi` Go library. It provides a session-based coding agent runtime with one built-in tool: `bash`.

## requirements

- Go `1.26+`
- An auth credential for your chosen mode (see below)

## install

```bash
go get github.com/zahlmann/phi
```

## auth modes

- OpenAI API key mode (default):
  set `OPENAI_API_KEY` or pass `RuntimeOptions.APIKey`.
- ChatGPT mode:
  set `PHI_CHATGPT_ACCESS_TOKEN` (and optional `PHI_CHATGPT_ACCOUNT_ID`) or pass `RuntimeOptions.AccessToken`/`RuntimeOptions.AccountID`, and set `RuntimeOptions.AuthMode = provider.AuthModeChatGPT`.

## minimal usage flow

1. Create a runtime with `phi.NewRuntime`.
2. Subscribe to events with `rt.Subscribe`.
3. Start a session with `rt.StartSession`.
4. Send follow-up prompts with `rt.QueueMessage`.
5. Close runtime with `rt.Close`.

```go
package main

import (
	"context"
	"fmt"

	"github.com/zahlmann/phi"
	"github.com/zahlmann/phi/ai/provider"
)

func main() {
	ctx := context.Background()

	rt := phi.NewRuntime(phi.RuntimeOptions{
		AuthMode:     provider.AuthModeOpenAIAPIKey,
		SystemPrompt: "You are a concise coding assistant.",
		WorkingDir:   ".",
	})
	defer rt.Close()

	rt.Subscribe(func(ev phi.Event) {
		if ev.Type == phi.EventFinalMessage && ev.AssistantMessage != nil {
			fmt.Printf("assistant event: %+v\n", *ev.AssistantMessage)
		}
	})

	started, err := rt.StartSession(ctx, phi.StartSessionRequest{
		Prompt: "List files and summarize what you find.",
	})
	if err != nil {
		panic(err)
	}

	if err := rt.QueueMessage(ctx, phi.QueueMessageRequest{
		SessionID: started.SessionID,
		Prompt:    "Now include hidden files too.",
	}); err != nil {
		panic(err)
	}
}
```

## events

`phi` emits:

- `EventToolCallStarted`
- `EventToolCallFinished`
- `EventFinalMessage`

Use `EventFinalMessage` to capture assistant output for each processed turn.

## key runtime options

- `WorkingDir`: directory exposed to the `bash` tool.
- `SystemPrompt`: base behavior instructions for the assistant.
- `ModelID`: model override (`gpt-5.2-codex` default for API key mode, `gpt-5.3-codex` for ChatGPT mode).
- `QueueCapacity`: max queued messages per session.

## test

```bash
GOCACHE=/tmp/gocache go test ./...
```
