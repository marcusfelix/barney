// Package gitcmd runs git subprocesses and, when needed, authenticates a
// single subprocess for git-over-HTTPS using a caller-supplied GitHub
// token — never via on-disk credentials or the daemon's ambient
// environment. Tokens are per-event and short-lived (GitHub App
// installation tokens), so auth is scoped per call rather than configured
// once globally.
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
// command and its stderr output. Use this for commands that don't talk to a
// remote (e.g. reading a committed file via `git show`).
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	return run(ctx, dir, nil, args...)
}

// RunAuthed runs a git command the same way as Run, but with git-over-HTTPS
// authenticated for token — for clone/fetch/push calls that talk to a
// remote. The token is injected via a GIT_CONFIG_* env var scoped to this
// one subprocess; it never touches the remote URL or on-disk git config.
func RunAuthed(ctx context.Context, dir, token string, args ...string) (string, error) {
	return run(ctx, dir, AuthEnv(token), args...)
}

func run(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
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
// against GitHub for a single subprocess using token. Exported so callers
// that need the same auth for a non-git process in the same subprocess
// invocation (e.g. handing `gh` a GH_TOKEN) can reuse it. Empty for an empty
// token.
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
