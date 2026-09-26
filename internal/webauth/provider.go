// Package webauth implements OIDC authentication for web UI users.
package webauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/securecookie"
	"golang.org/x/oauth2"
)

// Config holds OIDC configuration
type Config struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	CookieSecret string
	SessionTTL   time.Duration
}

// Session holds user session data
type Session struct {
	Subject   string    `json:"sub"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"exp"`
}

// Provider manages OIDC authentication
type Provider struct {
	oauth2Config *oauth2.Config
	verifier     *oidc.IDTokenVerifier
	cookies      *securecookie.SecureCookie
	sessionTTL   time.Duration
}

// NewProvider creates a new OIDC provider
func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	if cfg.IssuerURL == "" {
		return nil, fmt.Errorf("OIDC issuer URL is required")
	}

	// Perform OIDC provider discovery
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to discover OIDC provider: %w", err)
	}

	// Verify OIDC configuration was found
	if provider == nil {
		return nil, fmt.Errorf("OIDC provider discovery returned nil")
	}

	// Create OAuth2 config
	oauth2Config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	// Create ID token verifier
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	// Decode cookie secret (should be base64-encoded) or auto-generate if not provided
	var cookieKey []byte
	if cfg.CookieSecret != "" {
		decoded, err := base64.StdEncoding.DecodeString(cfg.CookieSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to decode cookie secret: %w", err)
		}
		if len(decoded) < 32 {
			return nil, fmt.Errorf("cookie secret must be at least 32 bytes (got %d)", len(decoded))
		}
		cookieKey = decoded[:32]
	} else {
		// Auto-generate random 32-byte key if not provided
		// This is safe because the key only needs to be consistent for the session lifetime
		// Sessions are invalidated if the service restarts, which is acceptable for web UI
		randomBytes := make([]byte, 32)
		if _, err := rand.Read(randomBytes); err != nil {
			return nil, fmt.Errorf("failed to generate cookie secret: %w", err)
		}
		cookieKey = randomBytes
		slog.Warn("cookie secret not configured, generated random key (sessions will be invalidated on restart)")
	}

	// Create secure cookie encoder (authenticate + encrypt)
	cookies := securecookie.New(cookieKey, cookieKey)
	cookies.SetSerializer(securecookie.JSONEncoder{})

	return &Provider{
		oauth2Config: oauth2Config,
		verifier:     verifier,
		cookies:      cookies,
		sessionTTL:   cfg.SessionTTL,
	}, nil
}

// HandleLogin redirects to Keycloak login page
func (p *Provider) HandleLogin(w http.ResponseWriter, r *http.Request) {
	// Extract return_to parameter if provided
	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" {
		returnTo = "/"
	}

	// Generate random state for CSRF protection
	state := randomString(32)
	nonce := randomString(32)

	// Store state + nonce + return_to in a temporary signed cookie
	stateData := map[string]string{
		"state":     state,
		"nonce":     nonce,
		"return_to": returnTo,
	}

	encoded, err := p.cookies.Encode("_oidc_state", stateData)
	if err != nil {
		slog.Error("failed to encode state cookie", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "_oidc_state",
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600, // 10 minutes
	})

	// Redirect to Keycloak login
	authURL := p.oauth2Config.AuthCodeURL(state, oidc.Nonce(nonce))
	http.Redirect(w, r, authURL, http.StatusFound)
}

// HandleCallback processes Keycloak callback
func (p *Provider) HandleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get state and error from query params
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	errMsg := r.URL.Query().Get("error")

	if errMsg != "" {
		slog.Warn("OIDC error from provider", "error", errMsg)
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	}

	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	// Retrieve and verify state cookie
	cookie, err := r.Cookie("_oidc_state")
	if err != nil {
		slog.Warn("missing state cookie", "error", err)
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	stateData := make(map[string]string)
	if err := p.cookies.Decode("_oidc_state", cookie.Value, &stateData); err != nil {
		slog.Warn("failed to decode state cookie", "error", err)
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	if stateData["state"] != state {
		http.Error(w, "State mismatch", http.StatusBadRequest)
		return
	}

	returnTo := stateData["return_to"]
	if returnTo == "" {
		returnTo = "/"
	}

	// Exchange code for token
	token, err := p.oauth2Config.Exchange(ctx, code)
	if err != nil {
		slog.Error("failed to exchange code for token", "error", err)
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	}

	// Extract and verify ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "No ID token in response", http.StatusUnauthorized)
		return
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		slog.Error("failed to verify ID token", "error", err)
		http.Error(w, "Invalid ID token", http.StatusUnauthorized)
		return
	}

	// Verify nonce matches
	var claims struct {
		Nonce string `json:"nonce"`
		Email string `json:"email"`
	}
	if err := idToken.Claims(&claims); err != nil {
		slog.Error("failed to extract claims", "error", err)
		http.Error(w, "Invalid token claims", http.StatusUnauthorized)
		return
	}

	if claims.Nonce != stateData["nonce"] {
		http.Error(w, "Nonce mismatch", http.StatusBadRequest)
		return
	}

	// Create session
	session := Session{
		Subject:   idToken.Subject,
		Email:     claims.Email,
		ExpiresAt: time.Now().Add(p.sessionTTL),
	}

	// Encode session into cookie
	encoded, err := p.cookies.Encode("session", session)
	if err != nil {
		slog.Error("failed to encode session cookie", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:   "_oidc_state",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	// Redirect to return_to
	http.Redirect(w, r, returnTo, http.StatusFound)
}

// HandleLogout clears session
func (p *Provider) HandleLogout(w http.ResponseWriter, r *http.Request) {
	// Clear session cookie
	http.SetCookie(w, &http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	// Redirect to home
	http.Redirect(w, r, "/", http.StatusFound)
}

// CurrentSession returns the validated session from the request's session cookie,
// if present, unexpired, and correctly signed. It never mutates the response or
// redirects; callers use it to conditionally show auth-aware UI on otherwise-public pages.
func (p *Provider) CurrentSession(r *http.Request) (*Session, bool) {
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil, false
	}

	var session Session
	if err := p.cookies.Decode("session", cookie.Value, &session); err != nil {
		return nil, false
	}

	if time.Now().After(session.ExpiresAt) {
		return nil, false
	}

	return &session, true
}

// Middleware returns HTTP middleware that checks for valid session
func (p *Provider) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := p.CurrentSession(r); ok {
			next(w, r)
			return
		}

		// Session invalid/missing/expired; clear expired cookies if applicable
		cookie, err := r.Cookie("session")
		if err == nil {
			var session Session
			if decErr := p.cookies.Decode("session", cookie.Value, &session); decErr == nil && time.Now().After(session.ExpiresAt) {
				http.SetCookie(w, &http.Cookie{
					Name:   "session",
					Value:  "",
					Path:   "/",
					MaxAge: -1,
				})
			}
		}

		http.Redirect(w, r, "/auth/login?return_to="+r.RequestURI, http.StatusFound)
	}
}

// randomString generates a random base64 string
func randomString(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.URLEncoding.EncodeToString(b)[:length]
}
