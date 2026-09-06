// Package webhook implements GitHub webhook ingestion: HTTP handling,
// HMAC-SHA256 signature validation, and normalization of event payloads.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// EventType is the set of GitHub events Barney ingests.
type EventType string

const (
	EventIssues                   EventType = "issues"
	EventIssueComment             EventType = "issue_comment"
	EventPullRequest              EventType = "pull_request"
	EventPullRequestReviewComment EventType = "pull_request_review_comment"
	EventPush                     EventType = "push"
)

// IngestedEvents is the set of event types Barney processes.
var IngestedEvents = map[EventType]bool{
	EventIssues:                   true,
	EventIssueComment:             true,
	EventPullRequest:              true,
	EventPullRequestReviewComment: true,
	EventPush:                     true,
}

// NormalizedEvent is the generic event shape passed downstream.
type NormalizedEvent struct {
	EventType EventType
	// EventID is the X-GitHub-Delivery header value, GitHub's unique
	// identifier for a webhook delivery.
	EventID       string
	RepoOwner     string
	RepoName      string
	CloneURL      string
	DefaultBranch string
	RawPayload    map[string]interface{}
}

// Handler processes normalized events.
type Handler interface {
	HandleEvent(event *NormalizedEvent)
}

// Server is the HTTP listener for GitHub webhooks.
type Server struct {
	Secret  string
	Handler Handler
	// Wg, when non-nil, tracks in-flight event processing so callers can
	// drain events during shutdown.
	Wg *sync.WaitGroup
}

// NewServer creates a webhook server bound to a handler.
func NewServer(secret string, handler Handler) *Server {
	return &Server{Secret: secret, Handler: handler}
}

// HandlerFunc returns the http.Handler for the server.
func (s *Server) HandlerFunc() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", s.handleWebhook)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// VerifySignature validates a GitHub X-Hub-Signature-256 header against the
// request body using HMAC-SHA256 with the shared secret.
func VerifySignature(secret string, body []byte, signatureHeader string) bool {
	if secret == "" || !strings.HasPrefix(signatureHeader, "sha256=") {
		return false
	}
	given, err := hex.DecodeString(strings.TrimPrefix(signatureHeader, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), given)
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 25<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	signature := r.Header.Get("X-Hub-Signature-256")
	if !VerifySignature(s.Secret, body, signature) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	eventType := EventType(r.Header.Get("X-GitHub-Event"))
	if !IngestedEvents[eventType] {
		// Acknowledge but ignore unsupported events.
		w.WriteHeader(http.StatusOK)
		return
	}

	event, err := Normalize(eventType, r.Header.Get("X-GitHub-Delivery"), body)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to normalize payload: %v", err), http.StatusBadRequest)
		return
	}

	if s.Wg != nil {
		s.Wg.Add(1)
	}
	go func() {
		if s.Wg != nil {
			defer s.Wg.Done()
		}
		defer func() {
			if rec := recover(); rec != nil {
				fmt.Printf("[webhook] panic handling event %s %s: %v\n", event.EventType, event.EventID, rec)
			}
		}()
		s.Handler.HandleEvent(event)
	}()

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("accepted"))
}

// Normalize parses a raw GitHub payload into a NormalizedEvent. deliveryID is
// the X-GitHub-Delivery header value (GitHub's unique delivery identifier).
func Normalize(eventType EventType, deliveryID string, body []byte) (*NormalizedEvent, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid JSON payload: %w", err)
	}

	event := &NormalizedEvent{
		EventType:  eventType,
		EventID:    deliveryID,
		RawPayload: payload,
	}
	normalizeEventID(event)
	normalizeRepo(event)

	if event.DefaultBranch == "" {
		event.DefaultBranch = "main"
	}

	return event, nil
}

// normalizeEventID ensures every event resolves to a branch-safe identifier:
// the delivery header value, the payload action, or a placeholder.
func normalizeEventID(event *NormalizedEvent) {
	if event.EventID != "" {
		return
	}
	if action := str(event.RawPayload, "action"); action != "" {
		event.EventID = action
	} else {
		event.EventID = "unknown"
	}
}

// normalizeRepo fills the repository fields from payload.repository, using
// the owner login (or the login portion of full_name as fallback) and the
// clone URL (or an html_url-derived one).
func normalizeRepo(event *NormalizedEvent) {
	repo := mapAt(event.RawPayload, "repository")
	if repo == nil {
		return
	}
	event.RepoOwner = str(mapAt(repo, "owner"), "login")
	if event.RepoOwner == "" {
		if fullName := str(repo, "full_name"); fullName != "" {
			event.RepoOwner = strings.SplitN(fullName, "/", 2)[0]
		}
	}
	event.RepoName = str(repo, "name")
	event.DefaultBranch = str(repo, "default_branch")

	event.CloneURL = str(repo, "clone_url")
	if event.CloneURL == "" {
		if htmlURL := str(repo, "html_url"); htmlURL != "" {
			event.CloneURL = htmlURL + ".git"
		}
	}
}

// str returns the string value at key, or "" when absent or not a string.
func str(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// mapAt returns the nested object at key, or nil when absent.
func mapAt(m map[string]interface{}, key string) map[string]interface{} {
	obj, _ := m[key].(map[string]interface{})
	return obj
}
