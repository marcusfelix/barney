package gitcmd

import (
	"context"
	"os"
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
	env := authEnv("tok")
	if len(env) != 3 {
		t.Fatalf("len(authEnv) = %d, want 3", len(env))
	}
	if !strings.Contains(env[2], "AUTHORIZATION: basic ") {
		t.Errorf("extraheader = %q, want basic auth header", env[2])
	}
	if strings.Contains(strings.Join(env, " "), "tok") {
		t.Error("token must only appear base64-encoded")
	}
}

func TestConfigureAuth(t *testing.T) {
	for _, k := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GH_TOKEN"} {
		t.Setenv(k, "")
	}

	ConfigureAuth("tok")

	if got := os.Getenv("GH_TOKEN"); got != "tok" {
		t.Errorf("GH_TOKEN = %q, want tok", got)
	}
	if got := os.Getenv("GIT_CONFIG_COUNT"); got != "1" {
		t.Errorf("GIT_CONFIG_COUNT = %q, want 1", got)
	}
	if strings.Contains(os.Getenv("GIT_CONFIG_VALUE_0"), "tok") {
		t.Error("token must only appear base64-encoded")
	}

	// An empty token must be a no-op, never clearing existing auth.
	ConfigureAuth("")
	if got := os.Getenv("GH_TOKEN"); got != "tok" {
		t.Errorf("GH_TOKEN = %q after empty ConfigureAuth, want unchanged", got)
	}
}
