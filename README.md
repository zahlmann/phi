# phi

`phi` is a small Go coding-agent library.
It has one tool: `bash`.

## Quick Start

```go
import (
	"context"

	"github.com/zahlmann/phi"
	"github.com/zahlmann/phi/ai/provider"
)

func run(ctx context.Context) error {
	rt := phi.NewRuntime(phi.RuntimeOptions{
		AuthMode:     provider.AuthModeOpenAIAPIKey,
		SystemPrompt: "You are a concise coding assistant.",
		WorkingDir:   ".",
	})
	defer rt.Close()

	started, err := rt.StartSession(ctx, phi.StartSessionRequest{
		Prompt: "Use bash to list files, then summarize.",
	})
	if err != nil {
		return err
	}

	return rt.QueueMessage(ctx, phi.QueueMessageRequest{
		SessionID: started.SessionID,
		Prompt:    "Now include hidden files too.",
	})
}
```

## Auth

- API mode: set `OPENAI_API_KEY` (or pass `RuntimeOptions.APIKey`).
- ChatGPT mode: set `PHI_CHATGPT_ACCESS_TOKEN` (or pass `RuntimeOptions.AccessToken`).

## Files That Matter

- `phi.go`: public runtime API.
- `coding/bash/tool.go`: the only tool.
- `coding/sdk/session.go`: small session wrapper.
- `ai/provider/openai.go`: OpenAI streaming adapter.

## Test

```bash
GOCACHE=/tmp/gocache go test ./...
```
