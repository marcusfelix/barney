// Package workspace manages on-demand git repository workspaces with
// per-repository locking to serialize concurrent git operations.
package workspace

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/deploid/barney/internal/gitcmd"
)

const maxBranchSegment = 80

// Event describes the minimal event info the workspace manager needs.
type Event struct {
	EventType     string
	EventID       string
	RepoOwner     string
	RepoName      string
	CloneURL      string
	DefaultBranch string
	// PullRef, when set, is a remote ref (e.g. "pull/12/head") that the
	// event branch should be based on instead of the default branch. Used
	// for pull_request events so agents operate on the PR's code.
	PullRef string
}

// Manager provisions and locks repository workspaces under a root directory.
type Manager struct {
	Root string
	// Git runs the git subprocesses (auth is configured via its ExtraEnv).
	Git gitcmd.Runner

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewManager creates a workspace manager rooted at root.
func NewManager(root string) (*Manager, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace root: %w", err)
	}
	return &Manager{Root: abs, locks: make(map[string]*sync.Mutex)}, nil
}

// Path resolves the workspace path for an event:
// <workspaceRoot>/<RepoOwner>/<RepoName>.
func (m *Manager) Path(ev Event) string {
	return filepath.Join(m.Root, ev.RepoOwner, ev.RepoName)
}

// BranchName computes the isolated branch name for an event:
// barney/<event_type>-<event_id>.
func (m *Manager) BranchName(ev Event) string {
	return fmt.Sprintf("barney/%s-%s", sanitizeBranchSegment(ev.EventType), sanitizeBranchSegment(ev.EventID))
}

// LockFor returns the per-repository mutex. Callers must hold this lock for
// the entire pipeline (setup, agent execution, delivery) that operates on the
// workspace; Setup assumes the lock is already held.
func (m *Manager) LockFor(ev Event) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.Path(ev)
	lk, ok := m.locks[key]
	if !ok {
		lk = &sync.Mutex{}
		m.locks[key] = lk
	}
	return lk
}

// Setup prepares the workspace for an event: it clones the repository if the
// workspace does not exist (otherwise fetches origin), then force-checks out
// an isolated branch at the event's target ref, discarding any state left
// behind by previous events. The caller must hold the per-repo lock.
func (m *Manager) Setup(ctx context.Context, ev Event) (path string, branch string, err error) {
	path = m.Path(ev)
	branch = m.BranchName(ev)

	if err := m.syncRemote(ctx, ev, path); err != nil {
		return path, branch, err
	}

	startPoints := m.fetchStartPoints(ctx, ev, path)
	for _, sp := range startPoints {
		// -B creates or resets the branch at the start point; -f discards
		// modifications left behind by previous (possibly failed) events.
		if _, err := m.Git.Run(ctx, path, "checkout", "-f", "-B", branch, sp); err == nil {
			if _, err := m.Git.Run(ctx, path, "clean", "-fd"); err != nil {
				return path, branch, fmt.Errorf("clean workspace: %w", err)
			}
			return path, branch, nil
		}
	}
	return path, branch, fmt.Errorf("checkout event branch %s (tried start points %v)", branch, startPoints)
}

// syncRemote clones the repository into a fresh workspace, or fetches origin
// in an existing one. A failed clone is removed so the next event starts
// from a clean slate instead of a poisoned workspace.
func (m *Manager) syncRemote(ctx context.Context, ev Event, path string) error {
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		if _, err := m.Git.Run(ctx, path, "fetch", "origin"); err != nil {
			return fmt.Errorf("fetch origin: %w", err)
		}
		return nil
	}

	if ev.CloneURL == "" {
		return fmt.Errorf("event %s %s has no clone URL", ev.EventType, ev.EventID)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create workspace parent dir: %w", err)
	}
	if _, err := m.Git.Run(ctx, "", "clone", ev.CloneURL, path); err != nil {
		if rmErr := os.RemoveAll(path); rmErr != nil {
			log.Printf("[workspace] failed to clean up partial clone at %s: %v", path, rmErr)
		}
		return fmt.Errorf("clone %s: %w", ev.CloneURL, err)
	}
	return nil
}

// fetchStartPoints returns candidate refs for the event branch, most
// specific first: a fetched pull ref, then the default branch (remote and
// local). On pull-ref fetch failure it falls back to the default branch.
func (m *Manager) fetchStartPoints(ctx context.Context, ev Event, path string) []string {
	var startPoints []string
	if ev.PullRef != "" {
		if _, err := m.Git.Run(ctx, path, "fetch", "origin", ev.PullRef); err != nil {
			log.Printf("[workspace] failed to fetch %s, falling back to %s: %v", ev.PullRef, ev.DefaultBranch, err)
		} else {
			startPoints = append(startPoints, "FETCH_HEAD")
		}
	}
	return append(startPoints, "origin/"+ev.DefaultBranch, ev.DefaultBranch)
}

// sanitizeBranchSegment makes a string safe for use as a git branch segment.
func sanitizeBranchSegment(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		out = "unknown"
	}
	if len(out) > maxBranchSegment {
		out = out[:maxBranchSegment]
	}
	return out
}
