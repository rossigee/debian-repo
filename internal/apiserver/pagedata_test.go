package apiserver

import (
	"net/http"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/webauth"
)

func TestBasePageData(t *testing.T) {
	tests := []struct {
		name              string
		setupServer       func() *Server
		setupRequest      func(*Server) *http.Request
		expectOIDCEnabled bool
		expectAuth        bool
	}{
		{
			name: "no webauth configured",
			setupServer: func() *Server {
				return &Server{deps: Deps{WebAuth: nil}}
			},
			setupRequest: func(s *Server) *http.Request {
				return &http.Request{Header: http.Header{}}
			},
			expectOIDCEnabled: false,
			expectAuth:        false,
		},
		{
			name: "webauth configured, no session cookie",
			setupServer: func() *Server {
				// A non-nil Provider (even if not fully initialized) indicates OIDC is enabled
				p := &webauth.Provider{}
				return &Server{deps: Deps{WebAuth: p}}
			},
			setupRequest: func(s *Server) *http.Request {
				return &http.Request{Header: http.Header{}}
			},
			expectOIDCEnabled: true,
			expectAuth:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.setupServer()
			r := tt.setupRequest(s)

			pd := s.basePageData(r)

			if pd.OIDCEnabled != tt.expectOIDCEnabled {
				t.Errorf("OIDCEnabled: expected %v, got %v", tt.expectOIDCEnabled, pd.OIDCEnabled)
			}
			if pd.IsAuthenticated != tt.expectAuth {
				t.Errorf("IsAuthenticated: expected %v, got %v", tt.expectAuth, pd.IsAuthenticated)
			}
		})
	}
}
