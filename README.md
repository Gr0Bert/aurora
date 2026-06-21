# aurora-capcompute

Durable Aurora orchestration server built on `capcompute`.

All rights reserved.

The TinyGo guest in the sibling `aurora-brains` project owns the agent loop and
calls only `extism:host/compute/play`.
The Go host owns all side effects. Concrete LLM, internet, and MCP
implementations live in the sibling `aurora-dispatchers` project. The guest does
not use Extism built-in HTTP or Extism network policy.

Each model turn returns a JSON object containing an `actions` array. The guest executes batched
`internet.read` actions sequentially and sends their results back as one
aggregated observation array before asking the model for its next action batch.
For compatibility with imperfect model output, the guest also accepts
bare action arrays, whitespace-delimited action objects, and a JSON string
containing any accepted form.

## Layout

- `cmd/aurora-agent`: runnable host.
- `cmd/aurora-server`: long-lived REST/SSE agent server.
- `../aurora-brains/agent`: TinyGo/Wasm guest entrypoint `run`.
- `internal/agent`: process-lifetime thread, run, session, and journal ownership.
- `internal/server`: HTTP and SSE transport.
- `internal/host`: durable replay/task composition around registered dispatchers.
- `internal/storage/sqlite`: SQLite adapter behind Aurora storage interfaces.
- `internal/task`: webhook task coordination.

The previous `internal/llm` and `internal/internet` implementations have moved
to `../aurora-dispatchers`. Agent brain source and artifacts live in
`../aurora-brains`.

## Build And Test

```sh
GOCACHE=/tmp/aurora-capcompute-go-build go test ./...
sh ../aurora-brains/agent/build.sh
AURORA_LLM=fake AURORA_HTTP_ALLOW=GET:https://example.com go run ./cmd/aurora-agent
```

## Agent Server

The server compiles the guest once and keeps threads, physical Wasm sessions,
active play handles, and replay journals in memory for the lifetime of the
process:

```sh
sh ../aurora-brains/agent/build.sh

AURORA_LLM=openai \
go run ./cmd/aurora-server
```

The default address is `127.0.0.1:8080`; override it with
`AURORA_SERVER_ADDR`.

Create a thread with an immutable default manifest and send its first message:

```sh
curl -sS -X POST \
  -H 'Content-Type: application/json' \
  -d '{
    "manifest": {
      "version": 1,
      "system_prompt": "You are a careful research assistant.",
      "capabilities": [{
        "name": "internet.read",
        "settings": {
          "allow": ["https://go.dev"],
          "timeout_ms": 10000,
          "max_response_bytes": 65536
        }
      }]
    }
  }' \
  http://127.0.0.1:8080/v1/threads

curl -sS -X POST \
  -H 'Content-Type: application/json' \
  -d '{"content":"Research the Go 1.26 release."}' \
  http://127.0.0.1:8080/v1/threads/THREAD_ID/messages
```

Capability settings may be replaced for one run:

```json
{
  "content": "Read any required public source.",
  "capability_overrides": [{
    "name": "internet.read",
    "settings": {
      "allow": ["*"],
      "timeout_ms": 10000,
      "max_response_bytes": 65536
    }
  }]
}
```

`allow: ["*"]` permits GET requests to any HTTP or HTTPS origin. URL
credentials, non-textual responses, oversized bodies, non-GET methods, and
disallowed redirect targets remain protected by the host.

Inspect a run or its complete journal:

```sh
curl -sS http://127.0.0.1:8080/v1/runs/RUN_ID
curl -sS http://127.0.0.1:8080/v1/runs/RUN_ID/journal
curl -sS http://127.0.0.1:8080/v1/runs/RUN_ID/tasks
```

When a dispatcher yields, Aurora creates a durable task and the run enters
`waiting_for_task`. Resolve it with the task's single-purpose token:

```sh
curl -sS -X POST \
  -H 'Authorization: Bearer TASK_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"decision":"approved","actor":"operator"}' \
  http://127.0.0.1:8080/v1/tasks/TASK_ID/resolve
```

Decisions are `approved`, `denied`, `completed`, `failed`, and `cancelled`.
Approved tasks redispatch the immutable original call. Completed tasks use the
provided JSON `data` as the tool result. Resolution is idempotent.

Receive thread events:

```sh
curl -N http://127.0.0.1:8080/v1/threads/THREAD_ID/events
```

Stop or retry a run:

```sh
curl -sS -X POST http://127.0.0.1:8080/v1/runs/RUN_ID/stop

curl -sS -X POST \
  -H 'Content-Type: application/json' \
  -d '{"mode":"resume"}' \
  http://127.0.0.1:8080/v1/runs/RUN_ID/retry
```

`resume` preserves the journal, effective manifest, and physical yielded
session. `restart` replaces the journal and reruns the turn from scratch.
`restart` may include new `capability_overrides` for explicit privilege
escalation. Only the latest run in a thread can be retried.

Each user message creates a fresh Wasm session. The new session receives only
completed user/assistant message pairs from the thread, not previous tool calls
or downloaded pages. A yielded run remains the active thread run until retried
or stopped.

Capability metadata is exported by the exact configured dispatcher chain and
copied into the guest input. For every session, the guest appends a generated
tool-calling protocol to the user-supplied system prompt. It lists only the
tools exposed by that session's dispatcher, including their manifest-derived
descriptions and input schemas. The guest forwards model actions generically by
capability name. `final` remains the only guest-reserved action.

Manifest version 2 adds a registered `brain` reference. Version 1 manifests are
accepted and normalized to version 2. MCP dispatcher entries use names such as
`mcp.docs` and reference an operator-registered server:

```json
{
  "name": "mcp.docs",
  "settings": {"server_id": "docs", "tools": ["search"]}
}
```

The server has no authentication or CORS policy. Threads, runs, completed
conversation history, and replay journals are stored in SQLite and restored
after restart. Physical Wasm sessions and dispatcher instances are recreated.

For the standalone debug interface, run the sibling `aurora-ui` project. Its
dependency-free Node server serves the browser files and proxies `/v1` to this
server, avoiding any CORS requirement.

## Runtime Configuration

- `AURORA_LLM=fake|openai`, default `fake`.
- `AURORA_FAKE_READ_URL`, default `https://example.com`.
- `AURORA_HTTP_ALLOW`, CLI-only fallback manifest, for example `GET:https://example.com`.
- `AURORA_GUEST_WASM`, default `../aurora-brains/agent/agent.wasm`.
- `AURORA_BRAINS`, optional JSON object mapping registered brain IDs to Wasm
  paths.
- `AURORA_DEFAULT_BRAIN`, default `aurora-default@1`.
- `AURORA_MCP_SERVERS`, optional JSON object mapping operator-controlled MCP
  server IDs to stdio command configurations.
- `AURORA_SERVER_ADDR`, default `127.0.0.1:8080`.
- `AURORA_DB`, default `aurora.db`.
- `AURORA_TENANT_ID`, default `local`.
- `AURORA_WEBHOOK_SECRET`, required to remain stable across restarts for durable
  task webhook tokens; a development-only fallback is used locally.
- `AURORA_MESSAGE`, or pass the message as CLI arguments.

The guest has no step limit. The host controls execution through the
`capcompute.PlayHandle`: `Stop` forcefully terminates the current Wasm instance,
while a host `yield` outcome leaves the session available for replay.

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
