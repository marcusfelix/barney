// Package main is the Barney v0 orchestration daemon entrypoint.
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/deploid/barney/internal/ghapp"
	"github.com/deploid/barney/internal/gitcmd"
	"github.com/deploid/barney/internal/jsonutil"
	"github.com/deploid/barney/pkg/agent"
	"github.com/deploid/barney/pkg/manifest"
	"github.com/deploid/barney/pkg/webhook"
	"github.com/deploid/barney/pkg/workspace"
)

const (
	defaultEventTimeout = 30 * time.Minute
	// maxEventTimeout keeps events well under the 1-hour lifetime of a
	// GitHub App installation token, so the token used to set up the
	// workspace and handed to the agent can't expire mid-run.
	maxEventTimeout = 55 * time.Minute
)

// Config holds daemon configuration from flags or environment.
type Config struct {
	Port          string
	WebhookSecret string
	WorkspaceRoot string
	AppID         string
	AppPrivateKey []byte
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
	secret := fs.String("webhook-secret", os.Getenv(agent.EnvWebhookSecret), "GitHub webhook HMAC secret (required)")
	root := envOrFlag(fs, "workspace-root", "WORKSPACE_ROOT", "/var/lib/barney/workspaces", "Workspace storage root")
	appID := fs.String("app-id", os.Getenv(agent.EnvAppID), "GitHub App ID (required)")
	appKeyB64 := fs.String("app-private-key", os.Getenv(agent.EnvAppPrivateKey), "Base64-encoded PEM GitHub App private key (required)")
	timeoutStr := envOrFlag(fs, "event-timeout", "EVENT_TIMEOUT", "30m", "Per-event processing timeout (Go duration)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return nil, err
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil || timeout <= 0 {
		return nil, fmt.Errorf("invalid --event-timeout / EVENT_TIMEOUT value %q", *timeoutStr)
	}
	if timeout > maxEventTimeout {
		return nil, fmt.Errorf("--event-timeout / EVENT_TIMEOUT %q exceeds %s: GitHub App installation tokens expire after 1 hour", *timeoutStr, maxEventTimeout)
	}

	cfg := &Config{
		Port:          *port,
		WebhookSecret: *secret,
		WorkspaceRoot: *root,
		AppID:         *appID,
		EventTimeout:  timeout,
	}
	if cfg.WebhookSecret == "" {
		return nil, fmt.Errorf("--webhook-secret / WEBHOOK_SECRET is required")
	}
	if cfg.AppID == "" {
		return nil, fmt.Errorf("--app-id / APP_ID is required")
	}
	if *appKeyB64 == "" {
		return nil, fmt.Errorf("--app-private-key / APP_PRIVATE_KEY is required")
	}
	// Trim whitespace: a trailing newline (common from `$(cat file)`,
	// secret-store injection, or a manually-edited .env) would otherwise
	// fail decoding even though the underlying key is valid.
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(*appKeyB64))
	if err != nil {
		return nil, fmt.Errorf("--app-private-key / APP_PRIVATE_KEY: invalid base64: %w", err)
	}
	cfg.AppPrivateKey = key
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
	Auth         *ghapp.AppAuth
	EventTimeout time.Duration
}

// HandleEvent processes a normalized webhook event end-to-end: minting a
// repo-scoped installation token, workspace setup, manifest evaluation, and
// agent execution for every matched trigger. What the agent does with its
// bash access (commit, push, open a PR) is entirely up to the prompt;
// Barney never touches git delivery.
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

	if event.InstallationID == 0 || event.RepoID == 0 {
		log.Printf("event %s %s missing installation/repository id (not delivered via a GitHub App install?); skipping", ev.EventType, ev.EventID)
		return
	}
	token, err := o.Auth.InstallationToken(ctx, event.InstallationID, event.RepoID, timeout)
	if err != nil {
		log.Printf("mint installation token for %s/%s: %v", ev.RepoOwner, ev.RepoName, err)
		return
	}
	ev.Token = token

	lock := o.Workspace.LockFor(ev)
	lock.Lock()
	defer lock.Unlock()

	path, branch, err := o.Workspace.Setup(ctx, ev)
	if err != nil {
		log.Printf("workspace setup failed for %s/%s: %v", ev.RepoOwner, ev.RepoName, err)
		return
	}
	log.Printf("workspace ready at %s on branch %s", path, branch)

	baseBranch := baseBranchFor(event, ev.DefaultBranch)

	m, err := manifest.LoadFromRef(ctx, path, "origin/"+baseBranch)
	if err != nil {
		log.Printf("failed to load manifest for %s/%s: %v", ev.RepoOwner, ev.RepoName, err)
		return
	}
	if m == nil {
		log.Printf("no manifest on %s for %s/%s; skipping", baseBranch, ev.RepoOwner, ev.RepoName)
		return
	}

	matched := manifest.Process(ctx, m, string(event.EventType), event.EventID, event.RawPayload)
	if len(matched) == 0 {
		log.Printf("no triggers matched event %s %s", event.EventType, event.EventID)
		return
	}

	o.runTriggers(ctx, matched, event, ev, path, branch, baseBranch)
	log.Printf("event %s %s complete; delivery is up to the agent", event.EventType, event.EventID)
}

// runTriggers executes each matched trigger's agent sequentially in the event
// workspace with the BARNEY_* environment contract in place.
func (o *Orchestrator) runTriggers(ctx context.Context, matched []manifest.MatchedTrigger, event *webhook.NormalizedEvent, ev workspace.Event, path, branch, baseBranch string) {
	env := agentEnvFor(event, ev, branch, baseBranch)
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

// agentEnvFor builds the environment handed to every agent process: the
// BARNEY_* context a bash-driven workflow needs, plus GitHub auth scoped to
// this one event (ev.Token). Unlike a static PAT, the installation token is
// per-event and short-lived, so it's injected directly here rather than
// left for the agent to inherit from the daemon's ambient environment.
func agentEnvFor(event *webhook.NormalizedEvent, ev workspace.Event, branch, baseBranch string) map[string]string {
	env := map[string]string{
		"BARNEY_EVENT_TYPE":  string(event.EventType),
		"BARNEY_EVENT_ID":    event.EventID,
		"BARNEY_REPO":        ev.RepoOwner + "/" + ev.RepoName,
		"BARNEY_BRANCH":      branch,
		"BARNEY_BASE_BRANCH": baseBranch,
		"GH_TOKEN":           ev.Token,
	}
	for k, v := range gitcmd.AuthEnvMap(ev.Token) {
		env[k] = v
	}
	return env
}

// isPullEvent reports whether the event carries a pull_request payload.
func isPullEvent(t webhook.EventType) bool {
	return t == webhook.EventPullRequest || t == webhook.EventPullRequestReviewComment
}

// pullRefFor extracts a pull request ref (refs/pull/<n>/head) for
// pull_request-flavored events so agents operate on the PR's code. Returns
// "" for other events.
func pullRefFor(event *webhook.NormalizedEvent) string {
	if !isPullEvent(event.EventType) {
		return ""
	}
	if n := jsonutil.NumberAt(jsonutil.MapAt(event.RawPayload, "pull_request"), "number"); n > 0 {
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
	base := jsonutil.MapAt(jsonutil.MapAt(event.RawPayload, "pull_request"), "base")
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

	auth, err := ghapp.New(cfg.AppID, cfg.AppPrivateKey)
	if err != nil {
		log.Fatalf("github app auth: %v", err)
	}

	wsm, err := workspace.NewManager(cfg.WorkspaceRoot)
	if err != nil {
		log.Fatalf("workspace manager: %v", err)
	}

	registry := agent.NewRegistry()
	registry.Register(agent.NewOpenCodeHarness())

	orchestrator := &Orchestrator{
		Workspace:    wsm,
		Registry:     registry,
		Auth:         auth,
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
