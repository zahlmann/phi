# phi

`phi` is a minimal Go SDK for running a coding agent loop with:
- auto-generated sessions
- queued user messages
- bash-only tool execution

The public entrypoint is the root package:

```go
import "github.com/zahlmann/phi"
```

## Features

- Session API:
1. `StartSession(...)`
2. `QueueMessage(...)`

- Event API:
1. `tool_call_started`
2. `tool_call_finished`
3. `final_message`

- Auth modes:
1. OpenAI API key (`openai_api_key`)
2. ChatGPT backend (`chatgpt`)

- Provider path:
1. Responses API only

- Session storage:
1. in-memory only

- Reasoning:
1. fixed to `xhigh`

## Quick Start

```go
rt := phi.NewRuntime(phi.RuntimeOptions{
    AuthMode:     provider.AuthModeOpenAIAPIKey,
    SystemPrompt: "You are a concise coding assistant.",
    WorkingDir:   ".",
})
defer rt.Close()

resp, err := rt.StartSession(ctx, phi.StartSessionRequest{
    Prompt: "Use bash to list files, then summarize.",
})
if err != nil {
    panic(err)
}

_ = rt.QueueMessage(ctx, phi.QueueMessageRequest{
    SessionID: resp.SessionID,
    Prompt:    "Now include hidden files too.",
})
```

## Auth

OpenAI API key mode (default):

```bash
export OPENAI_API_KEY="..."
go run ./coding/examples/minimal
```

ChatGPT backend mode:

```bash
export PHI_AUTH_MODE=chatgpt
go run ./coding/examples/minimal
```

`phi` supports ChatGPT credential flow in `ai/auth/openai` (interactive device login + token store).

## Test

If your environment blocks default Go cache writes, set `GOCACHE` to a writable path:

```bash
GOCACHE=/tmp/gocache go test ./...
```

## Examples

```bash
go run ./coding/examples/minimal
go run ./coding/examples/full
go run ./coding/examples/local
```
