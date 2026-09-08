package gitcmd

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunOutputs(t *testing.T) {
	out, err := Run(context.Background(), "", "version")
	if err != nil {
		t.Fatalf("Run(version) error = %v", err)
	}
	if !strings.Contains(out, "git version") {
		t.Errorf("version output = %q, want it to name a git version", out)
	}
}

func TestRunErrorsIncludeCommandAndStderr(t *testing.T) {
	_, err := Run(context.Background(), "", "nosuchsubcommand")
	if err == nil {
		t.Fatal("Run() should fail for an unknown subcommand")
	}
	if !strings.Contains(err.Error(), "nosuchsubcommand") {
		t.Errorf("err = %v, want it to name the command", err)
	}
	if !strings.Contains(err.Error(), "is not a git command") {
		t.Errorf("err = %v, want it to include git's stderr", err)
	}
}

func TestAuthEnv(t *testing.T) {
	env := AuthEnv("tok")
	if len(env) != 3 {
		t.Fatalf("len(AuthEnv) = %d, want 3", len(env))
	}
	joined := strings.Join(env, " ")
	if !strings.Contains(joined, "AUTHORIZATION: basic ") {
		t.Errorf("AuthEnv = %v, want a basic auth header", env)
	}
	if strings.Contains(joined, "tok") {
		t.Error("token must only appear base64-encoded")
	}
}

func TestAuthEnvEmptyToken(t *testing.T) {
	if env := AuthEnv(""); env != nil {
		t.Errorf("AuthEnv(\"\") = %v, want nil", env)
	}
}

func TestAuthEnvMap(t *testing.T) {
	m := AuthEnvMap("tok")
	if m["GIT_CONFIG_COUNT"] != "1" {
		t.Errorf("GIT_CONFIG_COUNT = %q, want 1", m["GIT_CONFIG_COUNT"])
	}
	if !strings.Contains(m["GIT_CONFIG_VALUE_0"], "AUTHORIZATION: basic ") {
		t.Errorf("GIT_CONFIG_VALUE_0 = %q, want a basic auth header", m["GIT_CONFIG_VALUE_0"])
	}
	if strings.Contains(m["GIT_CONFIG_VALUE_0"], "tok") {
		t.Error("token must only appear base64-encoded")
	}
	if m2 := AuthEnvMap(""); m2 != nil {
		t.Errorf("AuthEnvMap(\"\") = %v, want nil", m2)
	}
}

func TestRunAuthedDoesNotLeakIntoProcessEnv(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	if _, err := Run(context.Background(), "", "init", "--initial-branch=main", dir); err != nil {
		t.Fatalf("git init: %v", err)
	}

	// RunAuthed's auth env is scoped to its own subprocess: it must not
	// persist in on-disk git config or leak into the test's own process env.
	if _, err := RunAuthed(context.Background(), dir, "bogus-token", "rev-parse", "--is-inside-work-tree"); err != nil {
		t.Fatalf("RunAuthed() error = %v", err)
	}
	if out, err := Run(context.Background(), dir, "config", "--get", "http.extraheader"); err == nil {
		t.Errorf("expected no persisted http.extraheader in on-disk config, got %q", out)
	}
	if got := os.Getenv("GIT_CONFIG_COUNT"); got != "" {
		t.Errorf("GIT_CONFIG_COUNT leaked into the test process env: %q", got)
	}
}
