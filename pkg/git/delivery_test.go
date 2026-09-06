package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deploid/barney/internal/gitcmd"
)

// runGitRepo runs a git command, failing the test on error.
func runGitRepo(t *testing.T, dir string, args ...string) {
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

// initWorkRepo creates a repo with a local bare origin, a clean tree, and
// main pushed; it returns the work tree path.
func initWorkRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bare := filepath.Join(dir, "origin.git")
	work := filepath.Join(dir, "work")
	runGitRepo(t, "", "init", "--bare", "--initial-branch=main", bare)
	runGitRepo(t, "", "init", "--initial-branch=main", work)
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitRepo(t, work, "add", ".")
	runGitRepo(t, work, "commit", "-m", "initial")
	runGitRepo(t, work, "remote", "add", "origin", bare)
	runGitRepo(t, work, "push", "-u", "origin", "main")
	return work
}

// dirty recreates the tracked file with new content so the tree has changes.
func dirty(t *testing.T, work string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// deliver delivers on main with a stub PR creator, returning the result.
func deliver(t *testing.T, engine *Engine, work string) (*DeliveryResult, error) {
	t.Helper()
	engine.CreatePR = func(ctx context.Context, workDir, token, title, body, base string) (string, error) {
		return "http://example.local/pr/1", nil
	}
	return engine.Deliver(context.Background(), DeliveryOptions{
		WorkDir:   work,
		Branch:    "main",
		EventType: "issues",
		EventID:   "1",
	})
}

func authorOf(t *testing.T, work string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", work, "log", "-1", "--format=%an <%ae>").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestCommitUsesConfiguredIdentity(t *testing.T) {
	work := initWorkRepo(t)
	dirty(t, work)

	engine := &Engine{
		CommitterName:  "custom-barney",
		CommitterEmail: "custom@example.com",
		Git:            gitcmd.Runner{},
	}
	if _, err := deliver(t, engine, work); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}

	if got := authorOf(t, work); got != "custom-barney <custom@example.com>" {
		t.Errorf("commit author = %q, want custom identity", got)
	}
}

func TestCommitUsesDefaultIdentity(t *testing.T) {
	work := initWorkRepo(t)
	dirty(t, work)

	engine := &Engine{Git: gitcmd.Runner{}}
	if _, err := deliver(t, engine, work); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}

	want := defaultCommitterName + " <" + defaultCommitterEmail + ">"
	if got := authorOf(t, work); got != want {
		t.Errorf("commit author = %q, want %q", got, want)
	}
}

func TestDeliverNoChanges(t *testing.T) {
	work := initWorkRepo(t)
	engine := &Engine{Git: gitcmd.Runner{}}

	result, err := engine.Deliver(context.Background(), DeliveryOptions{
		WorkDir:   work,
		Branch:    "main",
		EventType: "issues",
		EventID:   "1",
	})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if result.Changed {
		t.Error("Changed = true, want false for clean workspace")
	}
	if result.PullRequest != nil {
		t.Error("PullRequest should be nil when there are no changes")
	}
}

func TestDeliverPushesBranch(t *testing.T) {
	work := initWorkRepo(t)
	dirty(t, work)

	engine := &Engine{Git: gitcmd.Runner{}}
	if _, err := deliver(t, engine, work); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}

	bare := filepath.Join(filepath.Dir(work), "origin.git")
	out, err := exec.Command("git", "-C", bare, "for-each-ref", "--format=%(refname:short)", "refs/heads/main").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "main" {
		t.Errorf("remote refs = %q, want main", strings.TrimSpace(string(out)))
	}
}

func TestDeliverPRErrorSurfaces(t *testing.T) {
	work := initWorkRepo(t)
	dirty(t, work)

	engine := &Engine{Git: gitcmd.Runner{}}
	engine.CreatePR = func(ctx context.Context, workDir, token, title, body, base string) (string, error) {
		return "", errors.New("boom")
	}
	result, err := engine.Deliver(context.Background(), DeliveryOptions{
		WorkDir:   work,
		Branch:    "main",
		EventType: "issues",
		EventID:   "1",
	})
	if err == nil {
		t.Fatal("Deliver() should fail when PR creation fails")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want it to wrap the PR error", err)
	}
	if !result.Changed {
		t.Errorf("Changed = false, want true (commit and push did succeed)")
	}
}

func TestAuthEnv(t *testing.T) {
	if got := gitcmd.AuthEnv(""); got != nil {
		t.Errorf("AuthEnv(\"\") = %v, want nil", got)
	}

	env := gitcmd.AuthEnv("tok")
	if len(env) != 3 {
		t.Fatalf("len(AuthEnv) = %d, want 3", len(env))
	}
	if !strings.Contains(env[2], "AUTHORIZATION: basic ") {
		t.Errorf("extraheader = %q, want basic auth header", env[2])
	}
	if strings.Contains(strings.Join(env, " "), "tok") {
		t.Error("token must only appear base64-encoded")
	}
}
