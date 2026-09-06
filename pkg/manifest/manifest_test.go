package manifest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sampleManifest = `version: "v0"
triggers:
  - id: "label-task"
    event: "issues.opened"
    filter: "payload.issue.labels.exists(l, l.name == 'agent-task')"
    agent: "opencode"
    prompt_template: "Task: {{ .payload.issue.title }}"
  - id: "any-closed"
    event: "issues.closed"
    prompt_template: "Closed: {{ .payload.issue.number }}"
  - id: "comment-all"
    event: "issue_comment"
    filter: "payload.action == 'created'"
    agent: "opencode"
    prompt_template: "Comment by {{ .payload.comment.user.login }}: {{ .payload.comment.body }}"
`

func TestParse(t *testing.T) {
	m, err := Parse([]byte(sampleManifest))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if m.Version != "v0" {
		t.Errorf("Version = %q, want v0", m.Version)
	}
	if len(m.Triggers) != 3 {
		t.Fatalf("len(Triggers) = %d, want 3", len(m.Triggers))
	}
	if m.Triggers[0].ID != "label-task" {
		t.Errorf("Trigger ID = %q", m.Triggers[0].ID)
	}
	if m.Triggers[1].Agent != "opencode" {
		t.Errorf("default Agent = %q, want opencode", m.Triggers[1].Agent)
	}
}

func TestEvaluateFilter(t *testing.T) {
	payload := map[string]interface{}{
		"action": "opened",
		"issue": map[string]interface{}{
			"number": 7,
			"labels": []interface{}{
				map[string]interface{}{"name": "bug"},
				map[string]interface{}{"name": "agent-task"},
			},
		},
	}

	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"empty filter passes", "", true},
		{"label exists", `payload.issue.labels.exists(l, l.name == 'agent-task')`, true},
		{"label missing", `payload.issue.labels.exists(l, l.name == 'nope')`, false},
		{"action check", `payload.action == 'opened'`, true},
		{"number compare", `payload.issue.number > 5`, true},
		{"combined", `payload.action == 'opened' && payload.issue.number < 10`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EvaluateFilter(context.Background(), tc.expr, payload)
			if err != nil {
				t.Fatalf("EvaluateFilter() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("EvaluateFilter() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluateFilterParseError(t *testing.T) {
	if _, err := EvaluateFilter(context.Background(), "payload.action ===", map[string]interface{}{}); err == nil {
		t.Error("expected parse error for invalid CEL expression")
	}
}

func TestMatchesEvent(t *testing.T) {
	cases := []struct {
		spec      string
		eventType string
		action    string
		want      bool
	}{
		{"issues.opened", "issues", "opened", true},
		{"issues.opened", "issues", "closed", false},
		{"issues", "issues", "anything", true},
		{"issues.*", "issues", "closed", true},
		{"issue_comment", "issue_comment", "created", true},
		{"issues.opened", "pull_request", "opened", false},
	}

	for _, tc := range cases {
		got := MatchesEvent(Trigger{Event: tc.spec}, tc.eventType, tc.action)
		if got != tc.want {
			t.Errorf("MatchesEvent(%q, %q, %q) = %v, want %v", tc.spec, tc.eventType, tc.action, got, tc.want)
		}
	}
}

func TestRenderPrompt(t *testing.T) {
	payload := map[string]interface{}{
		"issue": map[string]interface{}{
			"title": "Fix the login bug",
		},
	}
	prompt, err := RenderPrompt(Trigger{ID: "t1", PromptTemplate: "Task: {{ .payload.issue.title }}"}, payload, "issues", "opened")
	if err != nil {
		t.Fatalf("RenderPrompt() error = %v", err)
	}
	if prompt != "Task: Fix the login bug" {
		t.Errorf("prompt = %q", prompt)
	}
}

func TestRenderPromptMissingTemplate(t *testing.T) {
	if _, err := RenderPrompt(Trigger{ID: "t1"}, map[string]interface{}{}, "issues", "1"); err == nil {
		t.Error("expected error for empty prompt_template")
	}
}

func TestProcess(t *testing.T) {
	m, err := Parse([]byte(sampleManifest))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	payload := map[string]interface{}{
		"action": "opened",
		"issue": map[string]interface{}{
			"title":  "Do the thing",
			"number": 7,
			"labels": []interface{}{
				map[string]interface{}{"name": "agent-task"},
			},
		},
	}

	matched := Process(context.Background(), m, "issues", "opened", payload)
	if len(matched) != 1 {
		t.Fatalf("len(matched) = %d, want 1", len(matched))
	}
	if matched[0].Trigger.ID != "label-task" {
		t.Errorf("matched trigger = %q, want label-task", matched[0].Trigger.ID)
	}
	if matched[0].Prompt != "Task: Do the thing" {
		t.Errorf("prompt = %q", matched[0].Prompt)
	}
}

func TestProcessFilterErrorSkipsTriggerOnly(t *testing.T) {
	manifestYAML := `version: "v0"
triggers:
  - id: "bad-filter"
    event: "issues.opened"
    filter: "payload.issue.labels.exists(l, l.name == 'x')"
    prompt_template: "A: {{ .payload.issue.title }}"
  - id: "no-filter"
    event: "issues.opened"
    prompt_template: "B: {{ .payload.issue.title }}"
`
	m, err := Parse([]byte(manifestYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// A push-shaped payload has no "issue" key: trigger 1's filter errors.
	// Trigger 2 (no filter) must still match.
	payload := map[string]interface{}{"action": "opened"}
	matched := Process(context.Background(), m, "issues", "opened", payload)
	if len(matched) != 1 {
		t.Fatalf("len(matched) = %d, want 1 (bad filter must not abort the event)", len(matched))
	}
	if matched[0].Trigger.ID != "no-filter" {
		t.Errorf("matched trigger = %q, want no-filter", matched[0].Trigger.ID)
	}
}

func TestLoadMissingManifest(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if m != nil {
		t.Error("expected nil manifest when file does not exist")
	}
}

func TestLoadExistingManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".barney"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".barney", "manifest.yaml"), []byte(sampleManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if m == nil || len(m.Triggers) != 3 {
		t.Fatalf("expected manifest with 3 triggers, got %+v", m)
	}
}
