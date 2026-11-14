package apiserver

import (
	"fmt"
	"net/http"
	"slices"

	"git.golder.lan/rossgolderltd/debian-repo/internal/config"
)

// rejectNotAllowed answers 400 and reports true when value is outside a
// non-empty allowlist. The message names the rejected value. An empty allowlist
// places no restriction.
func rejectNotAllowed(w http.ResponseWriter, kind, value string, allowed []string) bool {
	if len(allowed) == 0 || slices.Contains(allowed, value) {
		return false
	}
	http.Error(w, fmt.Sprintf("%s %q is not permitted by this server's configuration", kind, value), http.StatusBadRequest)
	return true
}

// maxDebSize returns the largest accepted package size in bytes.
func (s *Server) maxDebSize() int64 {
	if s.config != nil && s.config.Validation.MaxDebSizeBytes > 0 {
		return s.config.Validation.MaxDebSizeBytes
	}
	return config.DefaultMaxDebSizeBytes
}

// validationAllowlists returns the configured suite, component and architecture
// allowlists. Each is empty when unrestricted.
func (s *Server) validationAllowlists() (distributions, components, architectures []string) {
	if s.config == nil {
		return nil, nil, nil
	}
	v := s.config.Validation
	return v.AllowedDistributions, v.AllowedComponents, v.AllowedArchitectures
}

// rejectNotAllowedSuiteComponent checks a suite and a component against their
// allowlists. It reports true when the request was rejected.
func (s *Server) rejectNotAllowedSuiteComponent(w http.ResponseWriter, suite, component string) bool {
	distributions, components, _ := s.validationAllowlists()
	if rejectNotAllowed(w, "distribution", suite, distributions) {
		return true
	}
	return rejectNotAllowed(w, "component", component, components)
}
