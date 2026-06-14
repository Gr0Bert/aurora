# aurora-capcompute

Minimal Aurora-on-`capcompute` prototype.

All rights reserved.

The TinyGo guest owns the agent loop and calls only `extism:host/compute/play`.
The Go host owns all side effects: `llm.chat` and `internet.read`. The guest does
not use Extism built-in HTTP or Extism network policy.

## Layout

- `cmd/aurora-agent`: runnable host.
- `guest/agent.go`: TinyGo/Wasm guest entrypoint `run`.
- `internal/host`: `dispatcher.Call` handlers.
- `internal/llm`: fake and OpenAI-compatible LLM clients.
- `internal/internet`: allowlisted HTTP reads.

## Build And Test

```sh
GOCACHE=/tmp/aurora-capcompute-go-build go test ./...
sh guest/build.sh
AURORA_LLM=fake AURORA_HTTP_ALLOW=GET:https://example.com go run ./cmd/aurora-agent
```

## Runtime Configuration

- `AURORA_LLM=fake|openai`, default `fake`.
- `AURORA_FAKE_READ_URL`, default `https://example.com`.
- `AURORA_HTTP_ALLOW`, for example `GET:https://example.com,GET:https://docs.example.org`.
- `AURORA_GUEST_WASM`, default `guest/agent.wasm`.
- `AURORA_MESSAGE`, or pass the message as CLI arguments.
- `AURORA_MAX_STEPS`, default `4`.

OpenAI-compatible mode uses Chat Completions:

- `OPENAI_API_KEY`
- `OPENAI_BASE_URL`, default `https://api.openai.com/v1`
- `OPENAI_MODEL`, default `gpt-5.4-mini`
- `OPENAI_TIMEOUT`
- `OPENAI_MAX_RETRIES`
- `OPENAI_MAX_TOKENS`
- `OPENAI_TEMPERATURE`

Internet reads are v0 GET-only. Allowlist entries are exact origin matches with
lowercased host comparison and ports respected. Redirect targets must also match
the allowlist. Response bodies are bounded and binary-looking content types are
rejected.
