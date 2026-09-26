package acl

import (
	"testing"
)

func TestStaticAuthorizer_DefaultDeny(t *testing.T) {
	authorizer := NewStaticAuthorizer(map[string][]Grant{})

	req := Request{Identity: "test", Operation: OpUpload, Suite: "stable", Component: "main"}
	decision := authorizer.Authorize(req)

	if decision.Allowed {
		t.Error("Expected deny for unknown identity, got allow")
	}
}

func TestStaticAuthorizer_UnknownIdentity(t *testing.T) {
	authorizer := NewStaticAuthorizer(map[string][]Grant{
		"known": {
			{Repos: []string{"*"}, Suites: []string{"*"}, Components: []string{"*"}, Operations: []string{"*"}},
		},
	})

	req := Request{Identity: "unknown", Operation: OpUpload, Repo: "stable", Suite: "stable", Component: "main"}
	decision := authorizer.Authorize(req)

	if decision.Allowed {
		t.Error("Expected deny for unknown identity, got allow")
	}
}

func TestStaticAuthorizer_ExactMatch(t *testing.T) {
	authorizer := NewStaticAuthorizer(map[string][]Grant{
		"test": {
			{Repos: []string{"default"}, Suites: []string{"stable"}, Components: []string{"main"}, Operations: []string{"upload"}},
		},
	})

	req := Request{Identity: "test", Operation: OpUpload, Repo: "default", Suite: "stable", Component: "main"}
	decision := authorizer.Authorize(req)

	if !decision.Allowed {
		t.Error("Expected allow for exact match, got deny")
	}
}

func TestStaticAuthorizer_WildcardSuite(t *testing.T) {
	authorizer := NewStaticAuthorizer(map[string][]Grant{
		"test": {
			{Repos: []string{"*"}, Suites: []string{"*"}, Components: []string{"main"}, Operations: []string{"upload"}},
		},
	})

	req := Request{Identity: "test", Operation: OpUpload, Repo: "default", Suite: "testing", Component: "main"}
	decision := authorizer.Authorize(req)

	if !decision.Allowed {
		t.Error("Expected allow for wildcard suite, got deny")
	}
}

func TestStaticAuthorizer_WildcardComponent(t *testing.T) {
	authorizer := NewStaticAuthorizer(map[string][]Grant{
		"test": {
			{Repos: []string{"*"}, Suites: []string{"stable"}, Components: []string{"*"}, Operations: []string{"upload"}},
		},
	})

	req := Request{Identity: "test", Operation: OpUpload, Repo: "default", Suite: "stable", Component: "contrib"}
	decision := authorizer.Authorize(req)

	if !decision.Allowed {
		t.Error("Expected allow for wildcard component, got deny")
	}
}

func TestStaticAuthorizer_WildcardOperation(t *testing.T) {
	authorizer := NewStaticAuthorizer(map[string][]Grant{
		"test": {
			{Repos: []string{"*"}, Suites: []string{"stable"}, Components: []string{"main"}, Operations: []string{"*"}},
		},
	})

	req := Request{Identity: "test", Operation: OpRemove, Repo: "default", Suite: "stable", Component: "main"}
	decision := authorizer.Authorize(req)

	if !decision.Allowed {
		t.Error("Expected allow for wildcard operation, got deny")
	}
}

func TestStaticAuthorizer_MismatchSuite(t *testing.T) {
	authorizer := NewStaticAuthorizer(map[string][]Grant{
		"test": {
			{Repos: []string{"*"}, Suites: []string{"stable"}, Components: []string{"main"}, Operations: []string{"upload"}},
		},
	})

	req := Request{Identity: "test", Operation: OpUpload, Repo: "default", Suite: "testing", Component: "main"}
	decision := authorizer.Authorize(req)

	if decision.Allowed {
		t.Error("Expected deny for mismatched suite, got allow")
	}
}

func TestStaticAuthorizer_MismatchComponent(t *testing.T) {
	authorizer := NewStaticAuthorizer(map[string][]Grant{
		"test": {
			{Repos: []string{"*"}, Suites: []string{"stable"}, Components: []string{"main"}, Operations: []string{"upload"}},
		},
	})

	req := Request{Identity: "test", Operation: OpUpload, Repo: "default", Suite: "stable", Component: "contrib"}
	decision := authorizer.Authorize(req)

	if decision.Allowed {
		t.Error("Expected deny for mismatched component, got allow")
	}
}

func TestStaticAuthorizer_MismatchOperation(t *testing.T) {
	authorizer := NewStaticAuthorizer(map[string][]Grant{
		"test": {
			{Repos: []string{"*"}, Suites: []string{"stable"}, Components: []string{"main"}, Operations: []string{"upload"}},
		},
	})

	req := Request{Identity: "test", Operation: OpRemove, Repo: "default", Suite: "stable", Component: "main"}
	decision := authorizer.Authorize(req)

	if decision.Allowed {
		t.Error("Expected deny for mismatched operation, got allow")
	}
}

func TestStaticAuthorizer_SuiteLessOperation(t *testing.T) {
	authorizer := NewStaticAuthorizer(map[string][]Grant{
		"test": {
			{Repos: []string{"*"}, Suites: []string{}, Components: []string{}, Operations: []string{"reconcile"}},
		},
	})

	req := Request{Identity: "test", Operation: OpReconcile, Repo: "default", Suite: "", Component: ""}
	decision := authorizer.Authorize(req)

	if !decision.Allowed {
		t.Error("Expected allow for suite-less operation, got deny")
	}
}

func TestStaticAuthorizer_MultipleGrants(t *testing.T) {
	authorizer := NewStaticAuthorizer(map[string][]Grant{
		"test": {
			{Repos: []string{"*"}, Suites: []string{"stable"}, Components: []string{"main"}, Operations: []string{"upload"}},
			{Repos: []string{"*"}, Suites: []string{"testing"}, Components: []string{"main"}, Operations: []string{"upload"}},
		},
	})

	// First grant does not match
	req1 := Request{Identity: "test", Operation: OpUpload, Repo: "default", Suite: "testing", Component: "main"}
	decision1 := authorizer.Authorize(req1)
	if !decision1.Allowed {
		t.Error("Expected allow for second matching grant, got deny")
	}

	// Second grant does not match
	req2 := Request{Identity: "test", Operation: OpUpload, Repo: "default", Suite: "stable", Component: "main"}
	decision2 := authorizer.Authorize(req2)
	if !decision2.Allowed {
		t.Error("Expected allow for first matching grant, got deny")
	}
}

func TestStaticAuthorizer_CombinedWildcards(t *testing.T) {
	authorizer := NewStaticAuthorizer(map[string][]Grant{
		"test": {
			{Repos: []string{"*"}, Suites: []string{"*"}, Components: []string{"*"}, Operations: []string{"*"}},
		},
	})

	req := Request{Identity: "test", Operation: OpReconcile, Repo: "default", Suite: "any", Component: "any"}
	decision := authorizer.Authorize(req)

	if !decision.Allowed {
		t.Error("Expected allow for all wildcards, got deny")
	}
}
