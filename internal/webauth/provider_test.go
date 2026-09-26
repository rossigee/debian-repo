package webauth

import (
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/securecookie"
)

func TestCurrentSession(t *testing.T) {
	// Create a provider with a known cookie secret for testing
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	cookies := securecookie.New(key, key)
	cookies.SetSerializer(securecookie.JSONEncoder{})

	p := &Provider{
		cookies:    cookies,
		sessionTTL: 1 * time.Hour,
	}

	now := time.Now()
	validSession := Session{
		Subject:   "user123",
		Email:     "user@example.com",
		ExpiresAt: now.Add(1 * time.Hour),
	}
	expiredSession := Session{
		Subject:   "user456",
		Email:     "expired@example.com",
		ExpiresAt: now.Add(-1 * time.Hour),
	}

	tests := []struct {
		name          string
		setupRequest  func() *http.Request
		expectSession bool
		expectEmail   string
	}{
		{
			name: "no session cookie",
			setupRequest: func() *http.Request {
				return &http.Request{Header: http.Header{}}
			},
			expectSession: false,
		},
		{
			name: "garbage cookie value",
			setupRequest: func() *http.Request {
				r := &http.Request{Header: http.Header{}}
				r.AddCookie(&http.Cookie{Name: "session", Value: "garbage"})
				return r
			},
			expectSession: false,
		},
		{
			name: "valid unexpired session",
			setupRequest: func() *http.Request {
				r := &http.Request{Header: http.Header{}}
				encoded, _ := cookies.Encode("session", validSession)
				r.AddCookie(&http.Cookie{Name: "session", Value: encoded})
				return r
			},
			expectSession: true,
			expectEmail:   "user@example.com",
		},
		{
			name: "expired session",
			setupRequest: func() *http.Request {
				r := &http.Request{Header: http.Header{}}
				encoded, _ := cookies.Encode("session", expiredSession)
				r.AddCookie(&http.Cookie{Name: "session", Value: encoded})
				return r
			},
			expectSession: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.setupRequest()
			sess, ok := p.CurrentSession(r)

			if ok != tt.expectSession {
				t.Errorf("expected session=%v, got %v", tt.expectSession, ok)
			}

			if tt.expectSession {
				if sess.Email != tt.expectEmail {
					t.Errorf("expected email %q, got %q", tt.expectEmail, sess.Email)
				}
				if sess.Subject == "" {
					t.Errorf("expected non-empty Subject")
				}
			}
		})
	}
}
