package model

import (
	"time"
)

// AptUserStoreV1 is the serializable format for apt Basic-Auth credentials
type AptUserStoreV1 struct {
	FormatVersion int64     `json:"format_version"` // = 1
	GeneratedAt   time.Time `json:"generated_at"`
	Users         []AptUser `json:"users"`
}

// AptUser represents a basic-auth user for apt clients
type AptUser struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"` // bcrypt hash
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Disabled     bool      `json:"disabled"`
}
