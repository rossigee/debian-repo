package acl

// Grant is one authorization rule: identity X may perform Operations on Repos/Suites x Components.
// "*" in Repos/Suites/Components/Operations means "all".
type Grant struct {
	// Repos is the list of allowed repos, or ["*"] for all. Empty means no repos allowed (default-deny).
	Repos []string
	// Suites is the list of allowed suites, or ["*"] for all.
	Suites []string
	// Components is the list of allowed components, or ["*"] for all.
	Components []string
	// Operations is the list of allowed operations, or ["*"] for all.
	Operations []string
}

// StaticAuthorizer implements Authorizer from an in-memory, immutable set of
// grants per identity, built once at startup from config.
type StaticAuthorizer struct {
	grants map[string][]Grant // identity -> grants
}

// NewStaticAuthorizer creates a StaticAuthorizer with the given grants.
func NewStaticAuthorizer(grants map[string][]Grant) *StaticAuthorizer {
	return &StaticAuthorizer{grants: grants}
}

// Authorize returns a Decision for the given Request.
func (a *StaticAuthorizer) Authorize(req Request) Decision {
	grantList, exists := a.grants[req.Identity]
	if !exists {
		return Decision{Allowed: false, Reason: "identity not found"}
	}

	for _, grant := range grantList {
		if matchesGrant(grant, req) {
			return Decision{Allowed: true, Reason: "matched"}
		}
	}

	return Decision{Allowed: false, Reason: "no matching grant"}
}

func matchesGrant(grant Grant, req Request) bool {
	if !matchesList(grant.Operations, string(req.Operation)) {
		return false
	}

	// Check Repo dimension (new in Phase 2)
	// Empty Repos list means default-deny (no repos allowed)
	if len(grant.Repos) == 0 {
		return false
	}
	if !matchesList(grant.Repos, req.Repo) {
		return false
	}

	// Suite and Component are optional; if both empty, grant applies to all
	if req.Suite == "" && req.Component == "" {
		return true
	}

	if !matchesList(grant.Suites, req.Suite) {
		return false
	}

	if !matchesList(grant.Components, req.Component) {
		return false
	}

	return true
}

func matchesList(list []string, value string) bool {
	for _, item := range list {
		if item == "*" || item == value {
			return true
		}
	}
	return false
}
