// Package gitcmd runs git subprocesses and configures GitHub token
// authentication in the process environment, so every child process —
// Barney's git calls and the agents' bash workflows — authenticates the
// same way without on-disk credentials.
package gitcmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Run executes a git command in dir (empty means the current directory) and
// returns its stdout. A failing command yields an error that includes the
// command and its stderr output. The subprocess inherits the daemon
// environment, so auth configured by ConfigureAuth applies automatically.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

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

// ConfigureAuth exports git-over-HTTPS auth for the given GitHub token into
// the process environment, so it is inherited by every subprocess: Barney's
// clone/fetch calls as well as the agent's own commit/push/`gh` commands.
// The token never touches remote URLs or on-disk git config. It is a no-op
// for an empty token.
func ConfigureAuth(token string) {
	if token == "" {
		return
	}
	for _, kv := range authEnv(token) {
		k, v, _ := strings.Cut(kv, "=")
		os.Setenv(k, v)
	}
	if os.Getenv("GH_TOKEN") == "" {
		os.Setenv("GH_TOKEN", token)
	}
}

// authEnv returns environment variables that authenticate git-over-HTTPS
// against GitHub for any git subprocess (clone/fetch/push) using the given
// token.
func authEnv(token string) []string {
	b64 := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic " + b64,
	}
}
