package manifest

import (
	"context"
	"testing"
)

func TestCookbookExpressions(t *testing.T) {
	prPayload := map[string]interface{}{
		"action": "opened",
		"pull_request": map[string]interface{}{
			"draft": false,
			"title": "[barney] fix",
			"labels": []interface{}{
				map[string]interface{}{"name": "size/L"},
			},
			"base": map[string]interface{}{"ref": "main"},
		},
	}
	pushPayload := map[string]interface{}{
		"ref": "refs/heads/main",
		"commits": []interface{}{
			map[string]interface{}{"id": "abc"},
		},
	}
	issuePayload := map[string]interface{}{
		"action": "opened",
		"issue": map[string]interface{}{
			"title": "[barney] do it",
			"user":  map[string]interface{}{"login": "dependabot[bot]"},
			"labels": []interface{}{
				map[string]interface{}{"name": "agent-task"},
			},
		},
	}
	commentPayload := map[string]interface{}{
		"action": "created",
		"comment": map[string]interface{}{
			"body": "/barney do it",
			"user": map[string]interface{}{"type": "User"},
		},
	}

	cases := []struct {
		name    string
		expr    string
		payload map[string]interface{}
		want    bool
	}{
		{"label gate", "payload.issue.labels.exists(l, l.name == 'agent-task')", issuePayload, true},
		{"title prefix + label", "payload.action == 'opened' && payload.issue.title.startsWith('[barney]') && payload.issue.labels.exists(l, l.name == 'agent-task')", issuePayload, true},
		{"user match", "payload.issue.user.login == 'dependabot[bot]'", issuePayload, true},
		{"human comment", "payload.action == 'created' && payload.comment.user.type != 'Bot'", commentPayload, true},
		{"command body", "payload.comment.body.startsWith('/barney')", commentPayload, true},
		{"pr ready main", "payload.action in ['opened', 'synchronize'] && !payload.pull_request.draft && payload.pull_request.base.ref == 'main'", prPayload, true},
		{"pr size label", "payload.pull_request.labels.exists(l, l.name.startsWith('size/'))", prPayload, true},
		{"push main", "payload.ref == 'refs/heads/main'", pushPayload, true},
		{"push has commits", "size(payload.commits) > 0", pushPayload, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EvaluateFilter(context.Background(), tc.expr, tc.payload)
			if err != nil {
				t.Fatalf("EvaluateFilter(%q) error = %v", tc.expr, err)
			}
			if got != tc.want {
				t.Errorf("EvaluateFilter(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}
