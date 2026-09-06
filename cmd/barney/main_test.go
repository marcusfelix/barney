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

// mockHarness writes a file into the workspace to simulate agent work and
// records the execution options it was handed.
type mockHarness struct {
	runs chan agent.ExecutionOpts
}

func (m *mockHarness) ID() string { return "mock" }

func (m *mockHarness) Execute(ctx context.Context, opts agent.ExecutionOpts) (*agent.ExecutionResult, error) {
	if err := os.WriteFile(filepath.Join(opts.WorkDir, "agent-output.txt"), []byte(opts.Prompt), 0o644); err != nil {
		return nil, err
	}
	select {
	case m.runs <- opts:
	default:
	}
	return &agent.ExecutionResult{Stdout: "wrote agent-output.txt", ExitCode: 0}, nil
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

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestIntegrationWebhookToEndAgent(t *testing.T) {
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
	mock := &mockHarness{runs: make(chan agent.ExecutionOpts, 1)}
	registry := agent.NewRegistry()
	registry.Register(mock)

	orch := &Orchestrator{
		Workspace:    wsm,
		Registry:     registry,
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

	// The agent run is the last thing Barney does — no delivery step follows.
	var run agent.ExecutionOpts
	select {
	case run = <-mock.runs:
	case <-time.After(20 * time.Second):
		t.Fatal("agent never ran")
	}

	workspaceDir := filepath.Join(wsRoot, "local", repoName)

	// Agent ran in the workspace with the rendered prompt.
	if run.WorkDir != workspaceDir {
		t.Errorf("agent WorkDir = %q, want %q", run.WorkDir, workspaceDir)
	}
	content, err := os.ReadFile(filepath.Join(workspaceDir, "agent-output.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "Task: Integration task" {
		t.Errorf("agent file = %q, want %q", content, "Task: Integration task")
	}

	// Agent environment contract.
	branch := "barney/issues-" + deliveryID
	for k, want := range map[string]string{
		"BARNEY_EVENT_TYPE":  "issues",
		"BARNEY_EVENT_ID":    deliveryID,
		"BARNEY_REPO":        "local/" + repoName,
		"BARNEY_BRANCH":      branch,
		"BARNEY_BASE_BRANCH": "main",
	} {
		if got := run.Env[k]; got != want {
			t.Errorf("agent env %s = %q, want %q", k, got, want)
		}
	}

	// Event branch is unique per delivery ID and checked out in the workspace.
	if out := gitOut(t, workspaceDir, "branch", "--list", branch); !strings.Contains(out, branch) {
		t.Errorf("expected branch %q in workspace, got %q", branch, out)
	}

	// Barney must leave the agent's work uncommitted.
	if status := gitOut(t, workspaceDir, "status", "--porcelain"); status != "?? agent-output.txt" {
		t.Errorf("git status = %q, want only untracked agent-output.txt (Barney must not commit)", status)
	}

	// Barney must not push anything: the remote only knows main.
	if refs := gitOut(t, bareRepo, "for-each-ref", "--format=%(refname:short)", "refs/heads/"); refs != "main" {
		t.Errorf("remote refs = %q, want only main (Barney must not push)", refs)
	}
}

func TestAgentEnvForPullRequestUsesPRBase(t *testing.T) {
	event := &webhook.NormalizedEvent{
		EventType:     webhook.EventPullRequest,
		EventID:       "d-1",
		RepoOwner:     "acme",
		RepoName:      "demo",
		DefaultBranch: "main",
		RawPayload: map[string]interface{}{
			"pull_request": map[string]interface{}{
				"number": 7,
				"base":   map[string]interface{}{"ref": "develop"},
			},
		},
	}
	ev := workspace.Event{
		EventType:     "pull_request",
		EventID:       "d-1",
		RepoOwner:     "acme",
		RepoName:      "demo",
		DefaultBranch: "main",
		PullRef:       "pull/7/head",
	}

	env := agentEnvFor(event, ev, "barney/pull_request-d-1")
	if got := env["BARNEY_BASE_BRANCH"]; got != "develop" {
		t.Errorf("BARNEY_BASE_BRANCH = %q, want develop (PR base)", got)
	}
	if got := env["BARNEY_REPO"]; got != "acme/demo" {
		t.Errorf("BARNEY_REPO = %q, want acme/demo", got)
	}
	if got := pullRefFor(event); got != "pull/7/head" {
		t.Errorf("pullRefFor() = %q, want pull/7/head", got)
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
