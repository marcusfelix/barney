// Package main is the Barney v0 orchestration daemon entrypoint.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/deploid/barney/internal/gitcmd"
	"github.com/deploid/barney/pkg/agent"
	"github.com/deploid/barney/pkg/manifest"
	"github.com/deploid/barney/pkg/webhook"
	"github.com/deploid/barney/pkg/workspace"
)

const defaultEventTimeout = 30 * time.Minute

// Config holds daemon configuration from flags or environment.
type Config struct {
	Port          string
	WebhookSecret string
	WorkspaceRoot string
	GitHubToken   string
	EventTimeout  time.Duration
}

// envOrFlag returns a flag whose default comes from an environment variable.
func envOrFlag(fs *flag.FlagSet, flagName, envName, def, usage string) *string {
	val := os.Getenv(envName)
	if val == "" {
		val = def
	}
	return fs.String(flagName, val, usage)
}

// LoadConfig parses configuration from environment variables and CLI flags.
// Flags override environment; environment overrides defaults.
func LoadConfig() (*Config, error) {
	fs := flag.NewFlagSet("barney", flag.ExitOnError)
	port := envOrFlag(fs, "port", "PORT", "8080", "HTTP listen port")
	secret := fs.String("webhook-secret", os.Getenv("WEBHOOK_SECRET"), "GitHub webhook HMAC secret (required)")
	root := envOrFlag(fs, "workspace-root", "WORKSPACE_ROOT", "/var/lib/barney/workspaces", "Workspace storage root")
	token := fs.String("github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token for git/gh operations (required)")
	timeoutStr := envOrFlag(fs, "event-timeout", "EVENT_TIMEOUT", "30m", "Per-event processing timeout (Go duration)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return nil, err
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil || timeout <= 0 {
		return nil, fmt.Errorf("invalid --event-timeout / EVENT_TIMEOUT value %q", *timeoutStr)
	}

	cfg := &Config{
		Port:          *port,
		WebhookSecret: *secret,
		WorkspaceRoot: *root,
		GitHubToken:   *token,
		EventTimeout:  timeout,
	}
	if cfg.WebhookSecret == "" {
		return nil, fmt.Errorf("--webhook-secret / WEBHOOK_SECRET is required")
	}
	if cfg.GitHubToken == "" {
		return nil, fmt.Errorf("--github-token / GITHUB_TOKEN is required")
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	return cfg, nil
}

// Orchestrator wires webhook events to workspace setup, manifest evaluation,
// and agent execution. Each event holds the per-repo workspace lock for its
// entire pipeline. Delivery — commits, pushes, pull requests — is the agent's
// job, not Barney's.
type Orchestrator struct {
	Workspace    *workspace.Manager
	Registry     *agent.Registry
	EventTimeout time.Duration
}

// HandleEvent processes a normalized webhook event end-to-end: workspace
// setup, manifest evaluation, and agent execution for every matched trigger.
// What the agent does with its bash access (commit, push, open a PR) is
// entirely up to the prompt; Barney never touches git delivery.
func (o *Orchestrator) HandleEvent(event *webhook.NormalizedEvent) {
	timeout := o.EventTimeout
	if timeout <= 0 {
		timeout = defaultEventTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ev := workspace.Event{
		EventType:     string(event.EventType),
		EventID:       event.EventID,
		RepoOwner:     event.RepoOwner,
		RepoName:      event.RepoName,
		CloneURL:      event.CloneURL,
		DefaultBranch: event.DefaultBranch,
		PullRef:       pullRefFor(event),
	}
	log.Printf("event %s %s for %s/%s", ev.EventType, ev.EventID, ev.RepoOwner, ev.RepoName)

	lock := o.Workspace.LockFor(ev)
	lock.Lock()
	defer lock.Unlock()

	path, branch, err := o.Workspace.Setup(ctx, ev)
	if err != nil {
		log.Printf("workspace setup failed for %s/%s: %v", ev.RepoOwner, ev.RepoName, err)
		return
	}
	log.Printf("workspace ready at %s on branch %s", path, branch)

	m, err := manifest.Load(path)
	if err != nil {
		log.Printf("failed to load manifest for %s/%s: %v", ev.RepoOwner, ev.RepoName, err)
		return
	}
	if m == nil {
		log.Printf("no manifest in %s; skipping", path)
		return
	}

	matched := manifest.Process(ctx, m, string(event.EventType), event.EventID, event.RawPayload)
	if len(matched) == 0 {
		log.Printf("no triggers matched event %s %s", event.EventType, event.EventID)
		return
	}

	o.runTriggers(ctx, matched, event, ev, path, branch)
	log.Printf("event %s %s complete; delivery is up to the agent", event.EventType, event.EventID)
}

// runTriggers executes each matched trigger's agent sequentially in the event
// workspace with the BARNEY_* environment contract in place.
func (o *Orchestrator) runTriggers(ctx context.Context, matched []manifest.MatchedTrigger, event *webhook.NormalizedEvent, ev workspace.Event, path, branch string) {
	env := agentEnvFor(event, ev, branch)
	for _, mt := range matched {
		h, err := o.Registry.Get(mt.Trigger.Agent)
		if err != nil {
			log.Printf("trigger %q: %v", mt.Trigger.ID, err)
			continue
		}
		log.Printf("running trigger %q via agent %q", mt.Trigger.ID, mt.Trigger.Agent)
		if _, err := h.Execute(ctx, agent.ExecutionOpts{
			WorkDir: path,
			Prompt:  mt.Prompt,
			Env:     env,
		}); err != nil {
			log.Printf("trigger %q agent execution failed: %v", mt.Trigger.ID, err)
		}
	}
}

// agentEnvFor builds the BARNEY_* environment contract handed to every agent
// process: everything a bash-driven workflow needs to commit, push, and open
// pull requests on its own. GitHub auth (GITHUB_TOKEN/GH_TOKEN and
// git-over-HTTPS config) is inherited from the daemon environment.
func agentEnvFor(event *webhook.NormalizedEvent, ev workspace.Event, branch string) map[string]string {
	return map[string]string{
		"BARNEY_EVENT_TYPE":  string(event.EventType),
		"BARNEY_EVENT_ID":    event.EventID,
		"BARNEY_REPO":        ev.RepoOwner + "/" + ev.RepoName,
		"BARNEY_BRANCH":      branch,
		"BARNEY_BASE_BRANCH": baseBranchFor(event, ev.DefaultBranch),
	}
}

// isPullEvent reports whether the event carries a pull_request payload.
func isPullEvent(t webhook.EventType) bool {
	return t == webhook.EventPullRequest || t == webhook.EventPullRequestReviewComment
}

// mapAt returns the nested object at key, or nil when absent.
func mapAt(m map[string]interface{}, key string) map[string]interface{} {
	obj, _ := m[key].(map[string]interface{})
	return obj
}

// numberAt returns the numeric field key as an int, or 0 when absent. It
// accepts both float64 (JSON-decoded payloads) and native Go numbers.
func numberAt(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

// pullRefFor extracts a pull request ref (refs/pull/<n>/head) for
// pull_request-flavored events so agents operate on the PR's code. Returns
// "" for other events.
func pullRefFor(event *webhook.NormalizedEvent) string {
	if !isPullEvent(event.EventType) {
		return ""
	}
	if n := numberAt(mapAt(event.RawPayload, "pull_request"), "number"); n > 0 {
		return fmt.Sprintf("pull/%d/head", n)
	}
	return ""
}

// baseBranchFor returns the branch an agent should target for pull requests:
// the PR's own base for pull_request-flavored events, else the repository
// default branch.
func baseBranchFor(event *webhook.NormalizedEvent, defaultBranch string) string {
	if !isPullEvent(event.EventType) {
		return defaultBranch
	}
	base := mapAt(mapAt(event.RawPayload, "pull_request"), "base")
	if ref, ok := base["ref"].(string); ok && ref != "" {
		return ref
	}
	return defaultBranch
}

func main() {
	log.SetPrefix("[barney] ")
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("configuration: %v", err)
	}

	// Authenticated git and `gh` live in the process environment, so every
	// subprocess inherits them: Barney's clone/fetch calls and the agent's
	// own commit/push/PR workflows.
	gitcmd.ConfigureAuth(cfg.GitHubToken)

	wsm, err := workspace.NewManager(cfg.WorkspaceRoot)
	if err != nil {
		log.Fatalf("workspace manager: %v", err)
	}

	registry := agent.NewRegistry()
	registry.Register(agent.NewOpenCodeHarness())

	orchestrator := &Orchestrator{
		Workspace:    wsm,
		Registry:     registry,
		EventTimeout: cfg.EventTimeout,
	}

	var wg sync.WaitGroup
	server := webhook.NewServer(cfg.WebhookSecret, orchestrator)
	server.Wg = &wg

	addr := ":" + cfg.Port
	srv := &http.Server{Addr: addr, Handler: server.HandlerFunc()}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Printf("shutting down: draining in-flight events")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown: %v", err)
		}
		wg.Wait()
	}()

	log.Printf("listening on %s (workspaces: %s)", addr, cfg.WorkspaceRoot)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
