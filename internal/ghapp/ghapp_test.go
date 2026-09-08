package ghapp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
}

func TestNewParsesPKCS1AndPKCS8(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if _, err := New("123", pkcs1); err != nil {
		t.Errorf("New() with PKCS1 key: %v", err)
	}

	pkcs8der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8der})
	if _, err := New("123", pkcs8); err != nil {
		t.Errorf("New() with PKCS8 key: %v", err)
	}
}

func TestNewRejectsGarbage(t *testing.T) {
	if _, err := New("123", []byte("not a key")); err == nil {
		t.Error("expected error for non-PEM input")
	}
}

func decodeJWTSegment(t *testing.T, seg string) map[string]any {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		t.Fatalf("decode jwt segment: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal jwt segment: %v", err)
	}
	return out
}

func TestSignJWTShape(t *testing.T) {
	a, err := New("app-42", testKeyPEM(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	jwt, err := a.signJWT()
	if err != nil {
		t.Fatalf("signJWT() error = %v", err)
	}

	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d segments, want 3", len(parts))
	}

	header := decodeJWTSegment(t, parts[0])
	if header["alg"] != "RS256" {
		t.Errorf("header alg = %v, want RS256", header["alg"])
	}

	claims := decodeJWTSegment(t, parts[1])
	if claims["iss"] != "app-42" {
		t.Errorf("claims iss = %v, want app-42", claims["iss"])
	}
	exp, _ := claims["exp"].(float64)
	if time.Until(time.Unix(int64(exp), 0)) > jwtTTL {
		t.Errorf("claims exp too far in the future: %v", time.Unix(int64(exp), 0))
	}
}

// newTestServer returns a stub access-token endpoint and a counter of how
// many times it was hit, so cache-hit tests can assert on request count.
func newTestServer(t *testing.T, expiresIn time.Duration) (*httptest.Server, *int32, *[]byte) {
	t.Helper()
	var hits int32
	var lastBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("missing Bearer auth header, got %q", auth)
		}
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		lastBody = body

		resp := installationTokenResponse{
			Token:     "ghs_test-token",
			ExpiresAt: time.Now().Add(expiresIn),
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	return srv, &hits, &lastBody
}

func TestInstallationTokenMintsAndCaches(t *testing.T) {
	srv, hits, _ := newTestServer(t, time.Hour)
	defer srv.Close()

	a, err := New("app-1", testKeyPEM(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	a.BaseURL = srv.URL

	tok1, err := a.InstallationToken(t.Context(), 10, 20)
	if err != nil {
		t.Fatalf("InstallationToken() error = %v", err)
	}
	if tok1 != "ghs_test-token" {
		t.Errorf("token = %q, want ghs_test-token", tok1)
	}

	tok2, err := a.InstallationToken(t.Context(), 10, 20)
	if err != nil {
		t.Fatalf("InstallationToken() second call error = %v", err)
	}
	if tok2 != tok1 {
		t.Errorf("second token = %q, want cached %q", tok2, tok1)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("server hit %d times, want 1 (second call should be cached)", got)
	}
}

func TestInstallationTokenRefreshesNearExpiry(t *testing.T) {
	srv, hits, _ := newTestServer(t, 1*time.Minute) // inside refreshBefore, forces a re-mint every call
	defer srv.Close()

	a, err := New("app-1", testKeyPEM(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	a.BaseURL = srv.URL

	if _, err := a.InstallationToken(t.Context(), 10, 20); err != nil {
		t.Fatalf("InstallationToken() error = %v", err)
	}
	if _, err := a.InstallationToken(t.Context(), 10, 20); err != nil {
		t.Fatalf("InstallationToken() second call error = %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Errorf("server hit %d times, want 2 (near-expiry tokens must not be cached)", got)
	}
}

func TestInstallationTokenScopesRepositoryIDs(t *testing.T) {
	srv, _, lastBody := newTestServer(t, time.Hour)
	defer srv.Close()

	a, err := New("app-1", testKeyPEM(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	a.BaseURL = srv.URL

	if _, err := a.InstallationToken(t.Context(), 10, 999); err != nil {
		t.Fatalf("InstallationToken() error = %v", err)
	}

	var req map[string]any
	if err := json.Unmarshal(*lastBody, &req); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	ids, ok := req["repository_ids"].([]any)
	if !ok || len(ids) != 1 || ids[0].(float64) != 999 {
		t.Errorf("repository_ids = %v, want [999]", req["repository_ids"])
	}
}

func TestInstallationTokenDifferentReposDoNotShareCache(t *testing.T) {
	srv, hits, _ := newTestServer(t, time.Hour)
	defer srv.Close()

	a, err := New("app-1", testKeyPEM(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	a.BaseURL = srv.URL

	if _, err := a.InstallationToken(t.Context(), 10, 1); err != nil {
		t.Fatalf("InstallationToken() error = %v", err)
	}
	if _, err := a.InstallationToken(t.Context(), 10, 2); err != nil {
		t.Fatalf("InstallationToken() error = %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Errorf("server hit %d times, want 2 (different repos must not share a cache entry)", got)
	}
}

func TestMintInstallationTokenErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"not permitted"}`))
	}))
	defer srv.Close()

	a, err := New("app-1", testKeyPEM(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	a.BaseURL = srv.URL

	if _, err := a.InstallationToken(t.Context(), 10, 20); err == nil {
		t.Error("expected error on non-201 response")
	}
}
