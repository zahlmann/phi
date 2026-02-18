# phi

`phi` is a minimal agent for use in programs.

It is based on [badlogic/pi-mono](https://github.com/badlogic/pi-mono/), then heavily stripped to SDK-only use and rewritten in Go by Codex.

Implemented auth/provider modes:
- OpenAI API (`openai_api_key`) via `OPENAI_API_KEY`
- ChatGPT backend (`chatgpt`) via OAuth device login + bearer token

## Test

```bash
go test ./...
```

## Examples

```bash
go run ./coding/examples/minimal
go run ./coding/examples/local
```

## Auth Modes

OpenAI API key mode (default):

```bash
export OPENAI_API_KEY="..."
go run ./coding/examples/minimal
```

`coding/examples/minimal` uses `gpt-5.2-codex` in this mode.

ChatGPT backend mode:

```bash
export PHI_AUTH_MODE=chatgpt
export PHI_CHATGPT_LOGIN=1
go run ./coding/examples/minimal
```

`coding/examples/minimal` uses `gpt-5.3-codex` in this mode.

This uses the ChatGPT backend API (`https://chatgpt.com/backend-api/codex/responses`).
Tokens are stored at `~/.phi/chatgpt_tokens.json` (override with `PHI_CHATGPT_TOKEN_PATH`).
The interactive flow supports either device-code completion or manually pasting an access token.

Programmatic switch in `sdk.CreateSessionOptions`:

```go
authMode := provider.AuthModeOpenAIAPIKey // or provider.AuthModeChatGPT
modelID := "gpt-5.2-codex"
if authMode == provider.AuthModeChatGPT {
    modelID = "gpt-5.3-codex"
}

opts := sdk.CreateSessionOptions{
    ProviderClient: provider.NewOpenAIClient(),
    Model:          &model.Model{Provider: "openai", ID: modelID},
    AuthMode:       authMode,
    AccessToken:    "...", // optional if stored in ~/.phi/chatgpt_tokens.json
    AccountID:      "...", // optional
    APIKey:         "...", // used for openai_api_key mode
}
```

## Repo Layout

```text
phi
├── agent/   # minimal agent loop + queue
├── ai/      # model/provider/stream/auth layers
├── coding/  # sdk runtime, sessions, tools, examples
└── go.mod
```

## Mental Model

```text
your program
    -> phi (sdk)
        -> agent runtime
            -> ai provider (OpenAI API or ChatGPT backend API)
```
