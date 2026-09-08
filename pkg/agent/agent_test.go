package agent

import (
	"strings"
	"testing"
)

func TestFilteredEnvironStripsWebhookSecret(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", "s3cret")
	t.Setenv("GITHUB_TOKEN", "ghp_test")

	env := filteredEnviron()
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "WEBHOOK_SECRET") {
		t.Error("filteredEnviron() must strip WEBHOOK_SECRET")
	}
	if !strings.Contains(joined, "GITHUB_TOKEN=ghp_test") {
		t.Error("filteredEnviron() must keep other daemon environment variables")
	}
}
