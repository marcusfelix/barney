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
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/deploid/barney/internal/gitcmd"
	"github.com/deploid/barney/pkg/agent"
	"github.com/deploid/barney/pkg/git"
	"github.com/deploid/barney/pkg/manifest"
	"github.com/deploid/barney/pkg/webhook"
	"github.com/deploid/barney/pkg/workspace"
)

const defaultEventTimeout = 30 * time.Minute

// Config holds daemon configuration from flags or environment.
type Config struct {
	Port           string
	WebhookSecret  string
	WorkspaceRoot  string
	GitHubToken    string
	EventTimeout   time.Duration
	CommitterName  string
	CommitterEmail string
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
	committerName := fs.String("committer-name", os.Getenv("COMMITTER_NAME"), "Git author name for automated commits (optional)")
	committerEmail := fs.String("committer-email", os.Getenv("COMMITTER_EMAIL"), "Git author email for automated commits (optional)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return nil, err
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil || timeout <= 0 {
		return nil, fmt.Errorf("invalid --event-timeout / EVENT_TIMEOUT value %q", *timeoutStr)
	}

	cfg := &Config{
		Port:           *port,
		WebhookSecret:  *secret,
		WorkspaceRoot:  *root,
		GitHubToken:    *token,
		EventTimeout:   timeout,
		CommitterName:  *committerName,
		CommitterEmail: *committerEmail,
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

// Orchestrator wires webhook events to workspace setup, manifest processing,
// agent execution, and delivery. Each event holds the per-repo workspace
// lock for its entire pipeline.
type Orchestrator struct {
	Workspace    *workspace.Manager
	Engine       *manifest.Engine
	Registry     *agent.Registry
	Delivery     *git.Engine
	EventTimeout time.Duration
}

// HandleEvent processes a normalized webhook event end-to-end: workspace
// setup, manifest evaluation, agent execution for every matched trigger, and
// a single delivery (commit/push/PR) covering all changes made by the event.
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

	matched := o.Engine.Process(ctx, m, string(event.EventType), event.EventID, event.RawPayload)
	if len(matched) == 0 {
		log.Printf("no triggers matched event %s %s", event.EventType, event.EventID)
		return
	}

	if o.runTriggers(ctx, matched, event, path, branch) {
		o.deliver(ctx, event, ev, path, branch)
	}
}

// runTriggers executes each matched trigger's agent sequentially and reports
// whether at least one run succeeded.
func (o *Orchestrator) runTriggers(ctx context.Context, matched []manifest.MatchedTrigger, event *webhook.NormalizedEvent, path, branch string) bool {
	agentEnv := map[string]string{
		"BARNEY_EVENT_TYPE": string(event.EventType),
		"BARNEY_EVENT_ID":   event.EventID,
		"BARNEY_REPO":       event.RepoName,
		"BARNEY_BRANCH":     branch,
	}

	succeeded := 0
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
			Env:     agentEnv,
		}); err != nil {
			log.Printf("trigger %q agent execution failed: %v", mt.Trigger.ID, err)
			continue
		}
		succeeded++
	}

	if succeeded == 0 {
		log.Printf("no successful agent runs for event %s %s; skipping delivery", event.EventType, event.EventID)
	}
	return succeeded > 0
}

// deliver commits and pushes all changes made by the event's agents as a
// single delivery and opens a pull request.
func (o *Orchestrator) deliver(ctx context.Context, event *webhook.NormalizedEvent, ev workspace.Event, path, branch string) {
	ref := displayRef(event)
	delivery, err := o.Delivery.Deliver(ctx, git.DeliveryOptions{
		WorkDir:    path,
		Branch:     branch,
		EventType:  ev.EventType,
		EventID:    ref,
		BaseBranch: prBaseFor(event, ev.DefaultBranch),
	})
	if err != nil {
		log.Printf("delivery failed for event %s %s: %v", event.EventType, event.EventID, err)
		return
	}
	if delivery.PullRequest != nil {
		log.Printf("delivered PR for %s #%s: %s", event.EventType, ref, delivery.PullRequest.URL)
	} else {
		log.Printf("agent run produced no changes; nothing delivered")
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

// numberAt returns the numeric field key as an int, or 0 when absent.
func numberAt(m map[string]interface{}, key string) int {
	f, _ := m[key].(float64)
	return int(f)
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

// prBaseFor returns the PR base branch for pull_request-flavored events,
// falling back to the repository default branch.
func prBaseFor(event *webhook.NormalizedEvent, defaultBranch string) string {
	if !isPullEvent(event.EventType) {
		return defaultBranch
	}
	base := mapAt(mapAt(event.RawPayload, "pull_request"), "base")
	if ref, ok := base["ref"].(string); ok && ref != "" {
		return ref
	}
	return defaultBranch
}

// displayRef returns a human-meaningful reference for commit messages and PR
// titles: the issue or PR number when available, else the delivery ID.
func displayRef(event *webhook.NormalizedEvent) string {
	for _, key := range []string{"issue", "pull_request"} {
		if n := numberAt(mapAt(event.RawPayload, key), "number"); n > 0 {
			return strconv.Itoa(n)
		}
	}
	return event.EventID
}

func main() {
	log.SetPrefix("[barney] ")
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("configuration: %v", err)
	}

	wsm, err := workspace.NewManager(cfg.WorkspaceRoot)
	if err != nil {
		log.Fatalf("workspace manager: %v", err)
	}
	wsm.Git = gitcmd.Runner{ExtraEnv: gitcmd.AuthEnv(cfg.GitHubToken)}

	registry := agent.NewRegistry()
	registry.Register(agent.NewOpenCodeHarness())

	delivery := git.NewEngine()
	delivery.Token = cfg.GitHubToken
	delivery.Git = wsm.Git
	delivery.CommitterName = cfg.CommitterName
	delivery.CommitterEmail = cfg.CommitterEmail

	orchestrator := &Orchestrator{
		Workspace:    wsm,
		Engine:       &manifest.Engine{},
		Registry:     registry,
		Delivery:     delivery,
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
