// Package ghapp mints short-lived, repository-scoped GitHub App installation
// tokens. It signs an App-level JWT with the App's private key, then
// exchanges it for an installation access token restricted to a single
// repository — so a token minted for one event can only ever reach the
// repository that event belongs to, and expires on its own within the hour.
package ghapp

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	defaultBaseURL = "https://api.github.com"
	// jwtTTL is the App-level JWT lifetime. GitHub allows at most 10 minutes;
	// this leaves margin against clock drift between us and GitHub.
	jwtTTL = 9 * time.Minute
	// refreshBefore is how far ahead of expiry a cached installation token is
	// considered stale and re-minted.
	refreshBefore = 5 * time.Minute
)

// AppAuth mints GitHub App installation tokens scoped to one repository at a
// time. The zero value is not usable; construct with New.
type AppAuth struct {
	AppID      string
	PrivateKey *rsa.PrivateKey

	// HTTPClient defaults to http.DefaultClient when nil.
	HTTPClient *http.Client
	// BaseURL defaults to the public GitHub API and exists for tests.
	BaseURL string

	mu    sync.Mutex
	cache map[string]cachedToken
}

type cachedToken struct {
	token  string
	expiry time.Time
}

// New creates an AppAuth from an App ID and a PEM-encoded RSA private key
// (PKCS#1 or PKCS#8, as downloaded from the App's settings page).
func New(appID string, privateKeyPEM []byte) (*AppAuth, error) {
	key, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	return &AppAuth{AppID: appID, PrivateKey: key, cache: make(map[string]cachedToken)}, nil
}

func parsePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in App private key")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse App private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("App private key is not RSA")
	}
	return rsaKey, nil
}

// InstallationToken returns a token scoped to a single repository within an
// installation, minting (or refreshing) it as needed. Tokens are cached per
// (installationID, repoID) and reused until close to expiry, so repeated
// events on the same repo don't each pay for a mint round-trip.
func (a *AppAuth) InstallationToken(ctx context.Context, installationID, repoID int64) (string, error) {
	key := fmt.Sprintf("%d/%d", installationID, repoID)

	a.mu.Lock()
	if c, ok := a.cache[key]; ok && time.Until(c.expiry) > refreshBefore {
		a.mu.Unlock()
		return c.token, nil
	}
	a.mu.Unlock()

	jwt, err := a.signJWT()
	if err != nil {
		return "", fmt.Errorf("sign app jwt: %w", err)
	}
	token, expiry, err := a.mintInstallationToken(ctx, jwt, installationID, repoID)
	if err != nil {
		return "", err
	}

	a.mu.Lock()
	a.cache[key] = cachedToken{token: token, expiry: expiry}
	a.mu.Unlock()

	return token, nil
}

// signJWT builds and signs the App-level JWT GitHub requires to authenticate
// as the App itself, ahead of exchanging it for an installation token.
func (a *AppAuth) signJWT() (string, error) {
	now := time.Now()
	header, err := base64JSON(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := base64JSON(map[string]any{
		"iat": now.Add(-30 * time.Second).Unix(), // allow for clock drift
		"exp": now.Add(jwtTTL).Unix(),
		"iss": a.AppID,
	})
	if err != nil {
		return "", err
	}

	signingInput := header + "." + claims
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.PrivateKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func base64JSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type installationTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// mintInstallationToken exchanges an App JWT for an installation access
// token restricted to repoID via the repository_ids parameter.
func (a *AppAuth) mintInstallationToken(ctx context.Context, jwt string, installationID, repoID int64) (string, time.Time, error) {
	body, err := json.Marshal(map[string]any{"repository_ids": []int64{repoID}})
	if err != nil {
		return "", time.Time{}, err
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", a.baseURL(), installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient().Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("request installation token: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("read installation token response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return "", time.Time{}, fmt.Errorf("mint installation token: %s: %s", resp.Status, string(respBody))
	}

	var parsed installationTokenResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", time.Time{}, fmt.Errorf("parse installation token response: %w", err)
	}
	return parsed.Token, parsed.ExpiresAt, nil
}

func (a *AppAuth) httpClient() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return http.DefaultClient
}

func (a *AppAuth) baseURL() string {
	if a.BaseURL != "" {
		return a.BaseURL
	}
	return defaultBaseURL
}
