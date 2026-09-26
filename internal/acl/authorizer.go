package acl

// Authorizer decides whether an identity may perform an operation on a suite/component.
// Implementations MUST default-deny: no matching grant => Decision{Allowed:false}.
type Authorizer interface {
	// Authorize returns a Decision for the given Request.
	Authorize(req Request) Decision
}
