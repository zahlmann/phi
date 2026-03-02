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

## Go vs Python CLI Agent Run

Use `phi` to run a more complex coding-agent task that builds a Go CLI and a Python CLI, executes both, benchmarks them, and writes a winner report with logs:

```bash
OPENAI_API_KEY=... scripts/run_phi_cli_compare.sh --iterations 1
```

Optional: choose an explicit human-readable trace log path:

```bash
OPENAI_API_KEY=... scripts/run_phi_cli_compare.sh --iterations 1 --human-log-path ./tmp_demo/phi_trace.log
```

Outputs are written under `benchmarks/results/<timestamp>_phi_cli_compare/`, including:
- `results.csv` and `summary.txt`
- per-run logs in `logs/`
- per-run readable traces in `*.human.log` (or your `--human-log-path`)
- generated workspace artifacts in `run_data/run_<n>/workspace/artifacts/`
