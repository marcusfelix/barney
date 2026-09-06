package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sign(t *testing.T, secret string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

const samplePayload = `{
  "action": "opened",
  "issue": {"id": 123, "number": 7, "title": "Do the thing"},
  "repository": {
    "id": 456,
    "name": "demo",
    "full_name": "acme/demo",
    "clone_url": "https://github.com/acme/demo.git",
    "html_url": "https://github.com/acme/demo",
    "default_branch": "main",
    "owner": {"login": "acme"}
  }
}`

func TestVerifySignature(t *testing.T) {
	secret := "hunter2"
	body := []byte(samplePayload)
	valid := sign(t, secret, body)

	cases := []struct {
		name      string
		secret    string
		signature string
		want      bool
	}{
		{"valid signature", secret, valid, true},
		{"wrong secret", "other", valid, false},
		{"missing prefix", secret, strings.TrimPrefix(valid, "sha256="), false},
		{"empty signature", secret, "", false},
		{"tampered body", secret, sign(t, secret, []byte(`{"action":"edited"}`)), false},
		{"empty secret", "", valid, false},
		{"non-hex signature", secret, "sha256=zzzz", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifySignature(tc.secret, body, tc.signature); got != tc.want {
				t.Errorf("VerifySignature() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	event, err := Normalize(EventIssues, "d-123", []byte(samplePayload))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if event.EventType != EventIssues {
		t.Errorf("EventType = %q, want %q", event.EventType, EventIssues)
	}
	if event.EventID != "d-123" {
		t.Errorf("EventID = %q, want d-123", event.EventID)
	}
	if event.RepoOwner != "acme" {
		t.Errorf("RepoOwner = %q, want %q", event.RepoOwner, "acme")
	}
	if event.RepoName != "demo" {
		t.Errorf("RepoName = %q, want %q", event.RepoName, "demo")
	}
	if event.CloneURL != "https://github.com/acme/demo.git" {
		t.Errorf("CloneURL = %q", event.CloneURL)
	}
	if event.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want %q", event.DefaultBranch, "main")
	}
	if event.RawPayload["action"] != "opened" {
		t.Errorf("RawPayload action = %v, want opened", event.RawPayload["action"])
	}
}

func TestNormalizeEventIDFallbacks(t *testing.T) {
	// No delivery header: fall back to the action.
	event, err := Normalize(EventIssues, "", []byte(samplePayload))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if event.EventID != "opened" {
		t.Errorf("EventID fallback = %q, want opened", event.EventID)
	}

	// No delivery header and no action: placeholder.
	event, err = Normalize(EventPush, "", []byte(`{"repository": {"name": "x"}}`))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if event.EventID != "unknown" {
		t.Errorf("EventID placeholder = %q, want unknown", event.EventID)
	}
}

func TestNormalizeCloneURLFallback(t *testing.T) {
	payload := `{"repository": {"name": "demo", "full_name": "acme/demo", "html_url": "https://github.com/acme/demo"}}`
	event, err := Normalize(EventPush, "", []byte(payload))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if event.CloneURL != "https://github.com/acme/demo.git" {
		t.Errorf("CloneURL fallback = %q", event.CloneURL)
	}
	if event.DefaultBranch != "main" {
		t.Errorf("DefaultBranch default = %q, want main", event.DefaultBranch)
	}
}

func TestHandleWebhookUnauthorized(t *testing.T) {
	handlerCalled := false
	srv := NewServer("secret", webhookHandlerFunc(func(e *NormalizedEvent) {
		handlerCalled = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(samplePayload))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	rec := httptest.NewRecorder()

	srv.HandlerFunc().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if handlerCalled {
		t.Error("handler should not be called on unauthorized request")
	}
}

func TestHandleWebhookAccepted(t *testing.T) {
	received := make(chan *NormalizedEvent, 1)
	srv := NewServer("secret", webhookHandlerFunc(func(e *NormalizedEvent) {
		received <- e
	}))

	body := []byte(samplePayload)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "6f1a2b3c-1111-2222-3333-444455556666")
	req.Header.Set("X-Hub-Signature-256", sign(t, "secret", body))
	rec := httptest.NewRecorder()

	srv.HandlerFunc().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	select {
	case event := <-received:
		if event.RepoName != "demo" {
			t.Errorf("RepoName = %q, want demo", event.RepoName)
		}
		if event.EventID != "6f1a2b3c-1111-2222-3333-444455556666" {
			t.Errorf("EventID = %q, want delivery header value", event.EventID)
		}
	case <-time.After(2 * time.Second):
		t.Error("handler was not called with the event")
	}
}

func TestHandleWebhookUnsupportedEvent(t *testing.T) {
	srv := NewServer("secret", webhookHandlerFunc(func(e *NormalizedEvent) {
		t.Error("handler should not be called for unsupported events")
	}))

	body := []byte(`{"zen": "Keep it simple."}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-Hub-Signature-256", sign(t, "secret", body))
	rec := httptest.NewRecorder()

	srv.HandlerFunc().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// webhookHandlerFunc adapts a function to the Handler interface.
type webhookHandlerFunc func(*NormalizedEvent)

func (f webhookHandlerFunc) HandleEvent(event *NormalizedEvent) { f(event) }
