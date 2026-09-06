// Package gitcmd runs git subprocesses with shared environment handling,
// providing one implementation of auth injection and error wrapping for all
// packages that shell out to git.
package gitcmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner executes git commands, optionally with extra environment variables
// (e.g. authentication).
type Runner struct {
	// ExtraEnv is added to every git subprocess environment.
	ExtraEnv []string
}

// Run executes a git command in dir (empty means the current directory) and
// returns its stdout. A failing command yields an error that includes the
// command and its stderr output.
func (r Runner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if r.ExtraEnv != nil {
		cmd.Env = append(os.Environ(), r.ExtraEnv...)
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err == nil {
		return string(out), nil
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), msg, err)
	}
	return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
}

// AuthEnv returns environment variables that authenticate git-over-HTTPS
// against GitHub for any git subprocess (clone/fetch/push) using the given
// token, without embedding the token in remote URLs or on-disk config.
func AuthEnv(token string) []string {
	if token == "" {
		return nil
	}
	b64 := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic " + b64,
	}
}
