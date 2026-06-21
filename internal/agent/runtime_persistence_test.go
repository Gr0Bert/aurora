package agent_test

import (
	"aurora-capcompute/internal/agent"
	aurorasqlite "aurora-capcompute/internal/storage/sqlite"
	"aurora-capcompute/internal/task"
	"aurora-dispatchers/llm"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPendingTaskSurvivesRuntimeRestart(t *testing.T) {
	if _, err := exec.LookPath("tinygo"); err != nil {
		t.Skip("tinygo not found")
	}
	wasmPath := buildPersistentTestBrain(t)
	dbPath := filepath.Join(t.TempDir(), "aurora.db")
	var reads atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("persistent approved content"))
	}))
	defer httpServer.Close()

	store1, err := aurorasqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open first store: %v", err)
	}
	runtime1, err := agent.NewRuntime(context.Background(), agent.Config{
		WasmPath:   wasmPath,
		LLM:        llm.NewFakeClient(httpServer.URL),
		Store:      store1,
		TaskSecret: []byte("stable-test-secret"),
	})
	if err != nil {
		t.Fatalf("new first runtime: %v", err)
	}
	settings, _ := json.Marshal(agent.InternetSettings{
		Allow: []string{httpServer.URL}, RequireApproval: true,
	})
	thread, err := runtime1.CreateThread(agent.Manifest{
		Version: agent.ManifestVersion,
		Capabilities: []agent.CapabilityConfig{{
			Name: "internet.read", Settings: settings,
		}},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	run, err := runtime1.CreateRun(thread.ID, "read after restart", nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	waitPersistentStatus(t, runtime1, run.ID, agent.RunWaitingTask)
	tasks, err := runtime1.Tasks(run.ID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks = %+v, err=%v", tasks, err)
	}
	token := tasks[0].WebhookToken
	closeRuntime(t, runtime1)

	store2, err := aurorasqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open second store: %v", err)
	}
	runtime2, err := agent.NewRuntime(context.Background(), agent.Config{
		WasmPath:   wasmPath,
		LLM:        llm.NewFakeClient(httpServer.URL),
		Store:      store2,
		TaskSecret: []byte("stable-test-secret"),
	})
	if err != nil {
		t.Fatalf("new second runtime: %v", err)
	}
	defer closeRuntime(t, runtime2)
	restored, err := runtime2.GetRun(run.ID)
	if err != nil {
		t.Fatalf("get restored run: %v", err)
	}
	if restored.Status != agent.RunWaitingTask {
		t.Fatalf("restored status = %s", restored.Status)
	}
	if _, err := runtime2.ResolveTask(tasks[0].ID, token, task.Resolution{
		Decision: task.StateApproved, Actor: "restart-test",
	}); err != nil {
		t.Fatalf("resolve restored task: %v", err)
	}
	completed := waitPersistentStatus(t, runtime2, run.ID, agent.RunCompleted)
	if !strings.Contains(completed.Answer, "persistent approved content") {
		t.Fatalf("answer = %q", completed.Answer)
	}
	if reads.Load() != 1 {
		t.Fatalf("reads = %d, want 1", reads.Load())
	}
}

func waitPersistentStatus(t *testing.T, runtime *agent.Runtime, runID string, want agent.RunStatus) agent.RunSnapshot {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		run, err := runtime.GetRun(runID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if run.Status == want {
			return run
		}
		if run.Status == agent.RunFailed {
			t.Fatalf("run failed: %s", run.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run did not reach %s", want)
	return agent.RunSnapshot{}
}

func closeRuntime(t *testing.T, runtime *agent.Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runtime.Close(ctx); err != nil {
		t.Fatalf("close runtime: %v", err)
	}
}

func buildPersistentTestBrain(t *testing.T) string {
	t.Helper()
	wasmPath := filepath.Join(t.TempDir(), "agent.wasm")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tinygo", "build",
		"-target", "wasip1",
		"-buildmode=c-shared",
		"-tags", "tinygo",
		"-o", wasmPath,
		"./agent",
	)
	cmd.Dir = "../../../aurora-brains"
	cmd.Env = append(os.Environ(),
		"XDG_CACHE_HOME="+t.TempDir(),
		"GOCACHE="+filepath.Join(t.TempDir(), "go-build"),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build brain: %v\n%s", err, out)
	}
	return wasmPath
}
