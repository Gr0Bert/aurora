package main

import (
	"bytes"
	"capcompute"
	"capcompute/session_store_memory"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	internalhost "aurora-capcompute/internal/host"
	"aurora-capcompute/internal/internet"
	"aurora-capcompute/internal/llm"
	extism "github.com/extism/go-sdk"
)

type Run struct {
	ID string
}

func (r Run) SessionKey() string {
	return r.ID
}

type agentInput struct {
	Message  string `json:"message"`
	MaxSteps int    `json:"max_steps"`
}

func main() {
	if err := execute(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, envDuration("AURORA_TIMEOUT", 2*time.Minute))
	defer cancel()

	llmClient, err := llmFromEnv()
	if err != nil {
		return err
	}
	policy, err := internet.ParseAllowlist(os.Getenv("AURORA_HTTP_ALLOW"))
	if err != nil {
		return fmt.Errorf("parse AURORA_HTTP_ALLOW: %w", err)
	}

	wasmPath := envDefault("AURORA_GUEST_WASM", "guest/agent.wasm")
	store := session_store_memory.New[string, Run]()
	compute, err := capcompute.NewComputeCompiledPlugin[string, Run](ctx, capcompute.Config[string, Run]{
		Manifest: extism.Manifest{
			Wasm: []extism.Wasm{extism.WasmFile{Path: wasmPath}},
		},
		PluginConfig: extism.PluginConfig{
			EnableWasi: true,
		},
		Dispatchers: internalhost.Factory[Run]{
			LLM:      llmClient,
			Internet: internet.NewClient(policy),
		},
		SessionStore: store,
	})
	if err != nil {
		return fmt.Errorf("compile guest %q: %w", wasmPath, err)
	}
	defer compute.CloseCompiled(context.Background())

	run := Run{ID: envDefault("AURORA_RUN_ID", fmt.Sprintf("run-%d", time.Now().UnixNano()))}
	input, err := json.Marshal(agentInput{
		Message:  messageFromArgs(args),
		MaxSteps: envInt("AURORA_MAX_STEPS", 4),
	})
	if err != nil {
		return fmt.Errorf("encode input: %w", err)
	}
	session, err := compute.CreateSession(ctx, capcompute.PlayRequest[string, Run]{
		Input:      input,
		Entrypoint: "run",
		UserData:   run,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	defer session.Close(context.Background())

	if err := store.SaveSession(ctx, run.SessionKey(), session); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	results, err := compute.Play(ctx, session)
	if err != nil {
		return fmt.Errorf("play: %w", err)
	}
	result := <-results
	if result.Status != capcompute.PlayCompleted {
		if result.Err != nil {
			return fmt.Errorf("play %s: %w", result.Status, result.Err)
		}
		return fmt.Errorf("play %s: exit %d output %s", result.Status, result.Exit, result.Output)
	}
	return writeJSON(os.Stdout, result.Output)
}

func llmFromEnv() (llm.Client, error) {
	switch strings.ToLower(envDefault("AURORA_LLM", "fake")) {
	case "fake":
		return llm.NewFakeClient(os.Getenv("AURORA_FAKE_READ_URL")), nil
	case "openai":
		return llm.NewOpenAIClient(llm.OpenAIConfigFromEnv())
	default:
		return nil, fmt.Errorf("unsupported AURORA_LLM: %s", os.Getenv("AURORA_LLM"))
	}
}

func messageFromArgs(args []string) string {
	if len(args) > 0 {
		return strings.Join(args, " ")
	}
	return envDefault("AURORA_MESSAGE", "Read https://example.com and summarize it.")
}

func writeJSON(file *os.File, raw json.RawMessage) error {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		_, err = file.Write(append(raw, '\n'))
		return err
	}
	pretty.WriteByte('\n')
	_, err := file.Write(pretty.Bytes())
	return err
}

func envDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err == nil {
		return parsed
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
