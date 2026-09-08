package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitRun runs git in dir with a fixed test identity, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// initBareRepo creates a bare "remote" repo seeded with one commit on main
// containing a marker.txt file, and returns its path.
func initBareRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	bare := filepath.Join(dir, "origin.git")
	seed := filepath.Join(dir, "seed")

	gitRun(t, "", "init", "--bare", "--initial-branch=main", bare)
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seed, "init", "--initial-branch=main", ".")
	if err := os.WriteFile(filepath.Join(seed, "marker.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seed, "add", ".")
	gitRun(t, seed, "commit", "-m", "initial")
	gitRun(t, seed, "push", "-u", bare, "main")
	return bare
}

func testEvent(bare, eventID string) Event {
	return Event{
		EventType:     "issues",
		EventID:       eventID,
		RepoOwner:     "local",
		RepoName:      "demo",
		CloneURL:      bare,
		DefaultBranch: "main",
	}
}

func TestSetupClonesAndChecksOutBranch(t *testing.T) {
	bare := initBareRepo(t)
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	ev := testEvent(bare, "d-1")

	path, branch, err := mgr.Setup(context.Background(), ev, "")
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if branch != "barney/issues-d-1" {
		t.Errorf("branch = %q, want barney/issues-d-1", branch)
	}
	if _, err := os.Stat(filepath.Join(path, "marker.txt")); err != nil {
		t.Errorf("expected marker.txt in workspace: %v", err)
	}
	if out := gitRun(t, path, "branch", "--show-current"); out != branch+"\n" {
		t.Errorf("checked-out branch = %q, want %q", out, branch)
	}
}

func TestSetupReusesExistingCloneAcrossEvents(t *testing.T) {
	bare := initBareRepo(t)
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	path1, branch1, err := mgr.Setup(context.Background(), testEvent(bare, "d-1"), "")
	if err != nil {
		t.Fatalf("Setup() first call error = %v", err)
	}
	path2, branch2, err := mgr.Setup(context.Background(), testEvent(bare, "d-2"), "")
	if err != nil {
		t.Fatalf("Setup() second call error = %v", err)
	}

	if path1 != path2 {
		t.Errorf("path changed across events: %q != %q, want the same workspace reused", path1, path2)
	}
	if branch1 == branch2 {
		t.Errorf("branch %q reused across distinct events, want a unique branch per event", branch1)
	}
}

func TestSetupCleansUntrackedFilesLeftByPreviousEvent(t *testing.T) {
	bare := initBareRepo(t)
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	path, _, err := mgr.Setup(context.Background(), testEvent(bare, "d-1"), "")
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	leftover := filepath.Join(path, "agent-output.txt")
	if err := os.WriteFile(leftover, []byte("leftover from a previous run"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := mgr.Setup(context.Background(), testEvent(bare, "d-2"), ""); err != nil {
		t.Fatalf("Setup() second call error = %v", err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("expected leftover file to be cleaned by the next event's Setup, stat err = %v", err)
	}
}

func TestSetupUsesPullRefWhenPresent(t *testing.T) {
	bare := initBareRepo(t)
	seed := t.TempDir()
	gitRun(t, "", "clone", bare, seed)
	gitRun(t, seed, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(seed, "marker.txt"), []byte("pr-head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seed, "commit", "-am", "pr commit")
	gitRun(t, seed, "push", bare, "feature:refs/pull/9/head")

	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	ev := testEvent(bare, "d-1")
	ev.EventType = "pull_request"
	ev.PullRef = "pull/9/head"

	path, _, err := mgr.Setup(context.Background(), ev, "")
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(path, "marker.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "pr-head\n" {
		t.Errorf("marker.txt = %q, want the PR ref's content, not the default branch's", content)
	}
}

func TestLockForReturnsSameMutexForSameRepoDifferentForOthers(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	evA := Event{RepoOwner: "acme", RepoName: "demo"}
	evB := Event{RepoOwner: "acme", RepoName: "other"}

	if mgr.LockFor(evA) != mgr.LockFor(evA) {
		t.Error("LockFor() must return the same mutex for the same repo")
	}
	if mgr.LockFor(evA) == mgr.LockFor(evB) {
		t.Error("LockFor() must return distinct mutexes for different repos")
	}
}

func TestBranchNameSanitizesUnsafeCharacters(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	ev := Event{EventType: "issue_comment", EventID: "weird id/with spaces:and*stars"}
	got := mgr.BranchName(ev)
	want := "barney/issue_comment-weird-id-with-spaces-and-stars"
	if got != want {
		t.Errorf("BranchName() = %q, want %q", got, want)
	}
}
