// Package aptauth implements HTTP Basic Authentication for apt clients.
package aptauth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/storage/minio"
	"golang.org/x/crypto/bcrypt"
)

// Store holds apt-user credentials in memory, read-heavy with periodic reloads from MinIO
type Store struct {
	mu    sync.RWMutex
	users map[string]model.AptUser // username -> user record
}

// NewStore creates a new apt auth store
func NewStore() *Store {
	return &Store{
		users: make(map[string]model.AptUser),
	}
}

// Load replaces the in-memory users map with a new set (called during reload)
func (s *Store) Load(users []model.AptUser) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.users = make(map[string]model.AptUser)
	for _, u := range users {
		s.users[u.Username] = u
	}
}

// Verify checks if the username and password are correct
// Returns false for any failure (unknown user, wrong password, disabled user)
// Uses constant-time password comparison via bcrypt to avoid timing attacks
func (s *Store) Verify(username, password string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[username]
	if !ok {
		// User not found; use dummy bcrypt hash comparison to avoid username enumeration
		// This takes ~the same time as a real password check
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcg7b3XeKeUxWdeS86E36CHhVBq"), []byte(password))
		return false
	}

	if user.Disabled {
		return false
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return false
	}

	return true
}

// ReloadFrom fetches the latest apt users from MinIO and loads them into memory
func (s *Store) ReloadFrom(ctx context.Context, minioStore *minio.AptUserStore) error {
	store, err := minioStore.GetUsers(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch apt users from MinIO: %w", err)
	}
	s.Load(store.Users)
	return nil
}

// StartReloadLoop starts a background ticker that reloads users from MinIO at regular intervals
// Never crashes on transient MinIO errors; only logs them
func (s *Store) StartReloadLoop(ctx context.Context, minioStore *minio.AptUserStore, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.ReloadFrom(ctx, minioStore); err != nil {
				slog.Error("failed to reload apt users", "error", err)
			}
		}
	}
}

// Middleware returns an http.HandlerFunc that wraps handler with Basic Auth validation
func Middleware(store *Store) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			username, password, ok := r.BasicAuth()
			if !ok || !store.Verify(username, password) {
				w.Header().Set("WWW-Authenticate", `Basic realm="debian-repo"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
}
