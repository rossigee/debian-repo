// Package version provides version information for the debian-repo service.
package version

var (
	// Version is the semantic version of the service.
	Version = "dev"
	// Commit is the git commit hash at build time.
	Commit = "unknown"
	// Date is the build timestamp.
	Date = "unknown"
)

// String returns a formatted version string with commit and build date.
func String() string {
	return Version + " (commit: " + Commit + ", built: " + Date + ")"
}
