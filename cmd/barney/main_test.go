package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deploid/barney/pkg/agent"
	"github.com/deploid/barney/pkg/git"
	"github.com/deploid/barney/pkg/manifest"
	"github.com/deploid/barney/pkg/webhook"
	"github.com/deploid/barney/pkg/workspace"
)

const deliveryID = "6f1a2b3c-1111-2222-3333-444455556666"

const integrationManifest = `version: "v0"
triggers:
  - id: "int-task"
    event: "issues.opened"
    filter: "payload.issue.labels.exists(l, l.name == 'agent-task')"
    agent: "mock"
    prompt_template: "Task: {{ .payload.issue.title }}"
`

// mockHarness writes a file into the workspace to simulate agent work.
type mockHarness struct{}

func (m *mockHarness) ID() string { return "mock" }

func (m *mockHarness) Execute(ctx context.Context, opts agent.ExecutionOpts) (*agent.ExecutionResult, error) {
	if err := os.WriteFile(filepath.Join(opts.WorkDir, "agent-output.txt"), []byte(opts.Prompt), 0o644); err != nil {
		return nil, err
	}
	return &agent.ExecutionResult{Stdout: "wrote agent-output.txt", ExitCode: 0}, nil
}

type prCall struct {
	title string
	body  string
	base  string
}

func initBareRepo(t *testing.T) (bare string, seed string) {
	t.Helper()
	dir := t.TempDir()
	bare = filepath.Join(dir, "origin.git")
	seed = filepath.Join(dir, "seed")

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=barney", "GIT_AUTHOR_EMAIL=barney@test",
			"GIT_COMMITTER_NAME=barney", "GIT_COMMITTER_EMAIL=barney@test",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	run("", "init", "--bare", "--initial-branch=main", bare)
	run(seed, "init", "--initial-branch=main", ".")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(seed, "add", ".")
	run(seed, "commit", "-m", "initial")
	run(seed, "push", "-u", bare, "main")
	return bare, seed
}

func seedManifest(t *testing.T, seed, bare string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(seed, ".barney"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, ".barney", "manifest.yaml"), []byte(integrationManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = seed
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=barney", "GIT_AUTHOR_EMAIL=barney@test",
			"GIT_COMMITTER_NAME=barney", "GIT_COMMITTER_EMAIL=barney@test",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", ".")
	run("commit", "-m", "add manifest")
	run("push", bare, "main")
}

func signBody(t *testing.T, secret string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestIntegrationWebhookToEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	secret := "test-secret"
	bareRepo, seed := initBareRepo(t)
	seedManifest(t, seed, bareRepo)
	repoName := strings.TrimSuffix(filepath.Base(bareRepo), ".git")

	wsRoot := t.TempDir()
	wsm, err := workspace.NewManager(wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	registry := agent.NewRegistry()
	registry.Register(&mockHarness{})

	prCreated := make(chan prCall, 1)
	delivery := git.NewEngine()
	delivery.Token = "dummy"
	delivery.CreatePR = func(ctx context.Context, workDir, token, title, body, base string) (string, error) {
		select {
		case prCreated <- prCall{title: title, body: body, base: base}:
		default:
		}
		return "http://example.local/pr/1", nil
	}

	orch := &Orchestrator{
		Workspace:    wsm,
		Engine:       &manifest.Engine{},
		Registry:     registry,
		Delivery:     delivery,
		EventTimeout: 5 * time.Minute,
	}

	server := webhook.NewServer(secret, orch)
	ts := httptest.NewServer(server.HandlerFunc())
	defer ts.Close()

	payload := []byte(fmt.Sprintf(`{
  "action": "opened",
  "issue": {"id": 99, "number": 7, "title": "Integration task", "labels": [{"name": "agent-task"}]},
  "repository": {
    "id": 1,
    "name": %q,
    "full_name": "local/%s",
    "clone_url": %q,
    "default_branch": "main",
    "owner": {"login": "local"}
  }
}`, repoName, repoName, bareRepo))
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/webhook", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", deliveryID)
	req.Header.Set("X-Hub-Signature-256", signBody(t, secret, payload))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook status = %d, want 200", resp.StatusCode)
	}

	// Delivery (commit + push + PR) happens after the agent run; wait for the
	// PR creation callback, which is the last step.
	var call prCall
	select {
	case call = <-prCreated:
	case <-time.After(20 * time.Second):
		t.Fatal("delivery never completed")
	}

	workspaceDir := filepath.Join(wsRoot, "local", repoName)

	// Agent ran with the rendered prompt.
	content, err := os.ReadFile(filepath.Join(workspaceDir, "agent-output.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "Task: Integration task" {
		t.Errorf("agent file = %q, want %q", content, "Task: Integration task")
	}

	// Event branch is unique per delivery ID.
	branch := "barney/issues-" + deliveryID
	out, err := exec.Command("git", "-C", workspaceDir, "branch", "--list", branch).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), branch) {
		t.Errorf("expected branch %q in workspace, got %q", branch, strings.TrimSpace(string(out)))
	}

	// Changes were committed on the event branch with the issue ref (#7).
	out, err = exec.Command("git", "-C", workspaceDir, "log", "-1", "--format=%s").Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := "barney: automated update for issues #7"; strings.TrimSpace(string(out)) != want {
		t.Errorf("commit message = %q, want %q", strings.TrimSpace(string(out)), want)
	}

	// Branch was pushed to the remote.
	out, err = exec.Command("git", "-C", bareRepo, "for-each-ref", "--format=%(refname:short)", "refs/heads/"+branch).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != branch {
		t.Errorf("remote does not contain branch %q: %q", branch, strings.TrimSpace(string(out)))
	}

	// PR was created against the default branch with the issue ref.
	if !strings.Contains(call.title, "issues #7") {
		t.Errorf("PR title = %q, want it to reference issues #7", call.title)
	}
	if call.base != "main" {
		t.Errorf("PR base = %q, want main", call.base)
	}
}

func TestLoadConfigRequiresSecrets(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	t.Setenv("WEBHOOK_SECRET", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("EVENT_TIMEOUT", "")
	os.Args = []string{"barney"}
	if _, err := LoadConfig(); err == nil {
		t.Error("expected error when webhook secret and token are missing")
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	t.Setenv("WEBHOOK_SECRET", "s3cret")
	t.Setenv("GITHUB_TOKEN", "ghp_test")
	t.Setenv("PORT", "9090")
	t.Setenv("WORKSPACE_ROOT", t.TempDir())
	t.Setenv("EVENT_TIMEOUT", "45m")
	os.Args = []string{"barney"}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.WebhookSecret != "s3cret" || cfg.GitHubToken != "ghp_test" || cfg.Port != "9090" {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg.EventTimeout != 45*time.Minute {
		t.Errorf("EventTimeout = %v, want 45m", cfg.EventTimeout)
	}
}

func TestLoadConfigInvalidTimeout(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	t.Setenv("WEBHOOK_SECRET", "s")
	t.Setenv("GITHUB_TOKEN", "t")
	t.Setenv("EVENT_TIMEOUT", "not-a-duration")
	os.Args = []string{"barney"}
	if _, err := LoadConfig(); err == nil {
		t.Error("expected error for invalid EVENT_TIMEOUT")
	}
}
