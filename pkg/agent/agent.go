// Package agent defines the pluggable agent harness interface and the
// OpenCode harness implementation.
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
	// Env holds extra environment variables for the agent process.
	Env map[string]string
	// Config holds harness-specific settings exposed to the agent as
	// prefixed environment variables (e.g. Config["model"] becomes
	// OPENCODE_MODEL for the OpenCode harness).
	Config map[string]string
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
// daemon environment plus any opts.Env and opts.Config variables, and
// returns the captured output.
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
	cmd.Env = append(os.Environ(), nameValueEnv(opts.Env)...)
	cmd.Env = append(cmd.Env, prefixedEnv("OPENCODE_", opts.Config)...)

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

// nameValueEnv converts a map to KEY=value environment entries.
func nameValueEnv(m map[string]string) []string {
	env := make([]string, 0, len(m))
	for k, v := range m {
		env = append(env, k+"="+v)
	}
	return env
}

// prefixedEnv converts a map to PREFIXEDKEY=value environment entries.
func prefixedEnv(prefix string, m map[string]string) []string {
	env := make([]string, 0, len(m))
	for k, v := range m {
		env = append(env, prefix+strings.ToUpper(k)+"="+v)
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
