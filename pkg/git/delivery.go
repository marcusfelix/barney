// Package git implements the post-agent delivery engine: change detection,
// commit, branch push, and pull request creation via the GitHub CLI.
package git

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/deploid/barney/internal/gitcmd"
)

const (
	defaultCommitterName  = "barney"
	defaultCommitterEmail = "barney@users.noreply.github.com"
)

// PullRequestInfo describes the PR created for an event.
type PullRequestInfo struct {
	Title string
	Body  string
	URL   string
}

// DeliveryResult captures the outcome of a delivery run.
type DeliveryResult struct {
	Changed       bool
	Branch        string
	CommitMessage string
	PullRequest   *PullRequestInfo
}

// DeliveryOptions describes a delivery run. Commit message and PR title are
// generated from EventType and EventID.
type DeliveryOptions struct {
	WorkDir   string
	Branch    string
	EventType string
	EventID   string
	// BaseBranch is the PR base. Defaults to the repository default branch
	// when empty.
	BaseBranch string
}

// PRCreator creates a pull request and returns its URL. It is injectable for
// testing; the default implementation shells out to the GitHub CLI.
type PRCreator func(ctx context.Context, workDir, token, title, body, base string) (string, error)

// Engine executes git delivery workflows.
type Engine struct {
	// Token is the GitHub token, passed to `gh` as GH_TOKEN.
	Token string
	// Git runs the git subprocesses (auth is configured via its ExtraEnv).
	Git gitcmd.Runner
	// CommitterName and CommitterEmail identify Barney in commit messages;
	// they fall back to the built-in defaults when empty.
	CommitterName  string
	CommitterEmail string
	// CreatePR overrides PR creation when set; defaults to the GitHub CLI.
	CreatePR PRCreator
}

// NewEngine creates a delivery engine.
func NewEngine() *Engine { return &Engine{} }

// Deliver runs the full delivery workflow: detect changes, stage, commit,
// push, and create a PR. When there are no changes it returns Changed=false
// and does nothing else.
func (e *Engine) Deliver(ctx context.Context, opts DeliveryOptions) (*DeliveryResult, error) {
	result := &DeliveryResult{Branch: opts.Branch}

	changed, err := e.hasChanges(ctx, opts.WorkDir)
	if err != nil {
		return nil, err
	}
	if !changed {
		log.Printf("[git] no changes in %s; nothing to deliver", opts.WorkDir)
		return result, nil
	}
	result.Changed = true

	// The commit message doubles as the PR title.
	msg := fmt.Sprintf("barney: automated update for %s #%s", opts.EventType, opts.EventID)
	result.CommitMessage = msg

	body := fmt.Sprintf("Automated update produced by Barney for event %s #%s.\n\nBranch: `%s`",
		opts.EventType, opts.EventID, opts.Branch)

	if err := e.commitAndPush(ctx, opts, msg); err != nil {
		return nil, err
	}

	createPR := e.CreatePR
	if createPR == nil {
		createPR = ghPRCreate
	}
	prURL, err := createPR(ctx, opts.WorkDir, e.Token, msg, body, opts.BaseBranch)
	if err != nil {
		return result, fmt.Errorf("create pull request: %w", err)
	}
	result.PullRequest = &PullRequestInfo{Title: msg, Body: body, URL: strings.TrimSpace(prURL)}
	log.Printf("[git] created pull request: %s", result.PullRequest.URL)
	return result, nil
}

// hasChanges reports whether the workspace has uncommitted changes.
func (e *Engine) hasChanges(ctx context.Context, workDir string) (bool, error) {
	out, err := e.Git.Run(ctx, workDir, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}

// commitAndPush stages everything, commits on the event branch, and pushes
// it to origin. The commit identity is set explicitly because daemon
// environments have no user gitconfig.
func (e *Engine) commitAndPush(ctx context.Context, opts DeliveryOptions, msg string) error {
	if _, err := e.Git.Run(ctx, opts.WorkDir, "add", "."); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if _, err := e.Git.Run(ctx, opts.WorkDir,
		"-c", "user.name="+e.committerName(),
		"-c", "user.email="+e.committerEmail(),
		"commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if _, err := e.Git.Run(ctx, opts.WorkDir, "push", "origin", opts.Branch); err != nil {
		return fmt.Errorf("git push origin %s: %w", opts.Branch, err)
	}
	return nil
}

// committerName returns the configured commit author name.
func (e *Engine) committerName() string {
	if e.CommitterName != "" {
		return e.CommitterName
	}
	return defaultCommitterName
}

// committerEmail returns the configured commit author email.
func (e *Engine) committerEmail() string {
	if e.CommitterEmail != "" {
		return e.CommitterEmail
	}
	return defaultCommitterEmail
}

// ghPRCreate runs `gh pr create` in workDir and returns the PR URL.
func ghPRCreate(ctx context.Context, workDir, token, title, body, base string) (string, error) {
	args := []string{"pr", "create", "--title", title, "--body", body}
	if base != "" {
		args = append(args, "--base", base)
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	if token != "" {
		cmd.Env = append(cmd.Env, "GH_TOKEN="+token)
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err == nil {
		return string(out), nil
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return "", fmt.Errorf("gh pr create: %s: %w", msg, err)
	}
	return "", fmt.Errorf("gh pr create: %w", err)
}
