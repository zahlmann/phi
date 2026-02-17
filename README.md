# phi

`phi` is a minimal agent for use in programs.

It is based on [badlogic/pi-mono](https://github.com/badlogic/pi-mono/), then heavily stripped to SDK-only use and rewritten in Go by Codex.

Currently implemented provider: OpenAI API.

## Test

```bash
go test ./...
```

## Examples

```bash
go run ./coding/examples/minimal
go run ./coding/examples/local
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
            -> ai provider (OpenAI API)
```
