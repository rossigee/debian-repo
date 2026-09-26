// Package acl provides authorization decisions for CI/CD bearer tokens.
package acl

// Operation identifies a logical action a CI token can be authorized for.
type Operation string

const (
	// OpUpload covers direct upload and presign+register operations.
	OpUpload Operation = "upload"
	// OpRemove is package removal from a distribution.
	OpRemove Operation = "remove"
	// OpUnprotect is bypassing protected-suite drain protection via force.
	// Draining a protected suite is more privileged than ordinary removal
	// and requires this distinct grant.
	OpUnprotect Operation = "unprotect"
	// OpReconcile is index reconciliation from pool contents.
	OpReconcile Operation = "reconcile"
	// OpListDists is listing available distributions.
	OpListDists Operation = "list-dists"
)

// Request describes what is being attempted, for an authorization decision.
type Request struct {
	// Identity is the CI token's identity (from lookup).
	Identity string
	// Operation is the logical action being attempted.
	Operation Operation
	// Repo is the target repository ID, empty for repo-less operations.
	Repo string
	// Suite is the target distribution suite, empty for suite-less operations.
	Suite string
	// Component is the target component, empty for suite-less operations.
	Component string
}

// Decision is the result of an authorization check.
type Decision struct {
	// Allowed is true if the request is authorized.
	Allowed bool
	// Reason is a log-only string explaining the decision.
	Reason string
}
