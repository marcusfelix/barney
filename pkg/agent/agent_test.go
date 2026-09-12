package agent

import (
	"strings"
	"testing"
)

func TestFilteredEnvironStripsDaemonSecrets(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", "s3cret")
	t.Setenv("APP_ID", "123456")
	t.Setenv("APP_PRIVATE_KEY", "-----BEGIN RSA PRIVATE KEY-----fake-----END RSA PRIVATE KEY-----")
	t.Setenv("SOME_OTHER_VAR", "keep-me")

	env := filteredEnviron()
	joined := strings.Join(env, "\n")
	for _, want := range []string{"WEBHOOK_SECRET", "APP_ID", "APP_PRIVATE_KEY"} {
		if strings.Contains(joined, want) {
			t.Errorf("filteredEnviron() must strip %s", want)
		}
	}
	if !strings.Contains(joined, "SOME_OTHER_VAR=keep-me") {
		t.Error("filteredEnviron() must keep other daemon environment variables")
	}
}
