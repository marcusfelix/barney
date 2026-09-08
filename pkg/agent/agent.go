// Package agent defines the pluggable agent harness interface and the
// OpenCode harness implementation. Agents run inside the event workspace
// with the daemon environment inherited; whatever they produce — commits,
// pushes, pull requests — is their business, done in bash.
package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// ExecutionOpts describes how and where to run an agent.
type ExecutionOpts struct {
	WorkDir string
	Prompt  string
	// Env holds extra environment variables for the agent process, on top
	// of the inherited daemon environment (which carries GitHub auth and
	// agent credentials).
	Env map[string]string
}

// ExecutionResult captures the outcome of an agent execution.
type ExecutionResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Harness is the extensible agent execution interface.
type Harness interface {
	ID() string
	Execute(ctx context.Context, opts ExecutionOpts) (*ExecutionResult, error)
}

// Registry holds available harnesses by ID.
type Registry struct {
	mu        sync.RWMutex
	harnesses map[string]Harness
}

// NewRegistry creates an empty harness registry.
func NewRegistry() *Registry {
	return &Registry{harnesses: make(map[string]Harness)}
}

// Register adds a harness to the registry.
func (r *Registry) Register(h Harness) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.harnesses[h.ID()] = h
}

// Get returns a harness by ID.
func (r *Registry) Get(id string) (Harness, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.harnesses[id]
	if !ok {
		return nil, fmt.Errorf("unknown agent harness %q", id)
	}
	return h, nil
}

// OpenCodeHarness executes prompts through the `opencode` CLI.
type OpenCodeHarness struct {
	// Bin is the opencode binary name or path. Defaults to "opencode".
	Bin string
	// LogPrefix tags execution log lines. Defaults to "agent".
	LogPrefix string
}

// NewOpenCodeHarness creates an OpenCode harness.
func NewOpenCodeHarness() *OpenCodeHarness {
	return &OpenCodeHarness{Bin: "opencode", LogPrefix: "agent"}
}

// ID returns the harness identifier.
func (h *OpenCodeHarness) ID() string { return "opencode" }

// Execute runs `opencode run <prompt>` inside opts.WorkDir, inheriting the
// daemon environment (GitHub auth, agent credentials) plus any opts.Env
// variables, and returns the captured output.
func (h *OpenCodeHarness) Execute(ctx context.Context, opts ExecutionOpts) (*ExecutionResult, error) {
	if strings.TrimSpace(opts.Prompt) == "" {
		return nil, fmt.Errorf("empty prompt")
	}
	if _, err := exec.LookPath(h.Bin); err != nil {
		return nil, fmt.Errorf("%q not found in PATH", h.Bin)
	}
	prefix := h.LogPrefix
	if prefix == "" {
		prefix = "agent"
	}

	cmd := exec.CommandContext(ctx, h.Bin, "run", opts.Prompt)
	cmd.Dir = opts.WorkDir
	cmd.Env = append(filteredEnviron(), nameValueEnv(opts.Env)...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("[%s] executing %s run in %s", prefix, h.Bin, opts.WorkDir)

	err := cmd.Run()
	res := &ExecutionResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode(err),
	}
	if res.Stdout != "" {
		log.Printf("[%s][stdout] %s", prefix, res.Stdout)
	}
	if res.Stderr != "" {
		log.Printf("[%s][stderr] %s", prefix, res.Stderr)
	}
	if err != nil {
		return res, fmt.Errorf("opencode execution failed (exit %d): %w", res.ExitCode, err)
	}
	return res, nil
}

// daemonOnlySecrets are environment variables the agent has no legitimate
// use for. Prompt injection (an untrusted issue/comment body telling the
// agent to run "env" or read its own process environment) would otherwise
// hand an attacker the means to forge webhook deliveries or, worse, mint
// GitHub App installation tokens for every repo the App can reach.
var daemonOnlySecrets = []string{"WEBHOOK_SECRET=", "APP_ID=", "APP_PRIVATE_KEY="}

// filteredEnviron returns the daemon's environment with daemonOnlySecrets
// stripped. The agent's own GitHub auth (a short-lived, repo-scoped
// installation token) is injected separately per event via opts.Env.
func filteredEnviron() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
outer:
	for _, kv := range env {
		for _, secret := range daemonOnlySecrets {
			if strings.HasPrefix(kv, secret) {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

// nameValueEnv converts a map to KEY=value environment entries.
func nameValueEnv(m map[string]string) []string {
	env := make([]string, 0, len(m))
	for k, v := range m {
		env = append(env, k+"="+v)
	}
	return env
}

// exitCode extracts an exit code from an exec.ExitError, returning -1 for
// other errors.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}
