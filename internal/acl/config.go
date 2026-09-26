package acl

import (
	"git.golder.lan/rossgolderltd/debian-repo/internal/config"
)

// FromConfig builds a StaticAuthorizer from config.CITokenConfig entries.
func FromConfig(tokens []config.CITokenConfig) *StaticAuthorizer {
	grants := make(map[string][]Grant)
	for _, token := range tokens {
		grantList := make([]Grant, 0, len(token.Grants))
		for _, gc := range token.Grants {
			grantList = append(grantList, Grant{
				Repos:      gc.Repos,
				Suites:     gc.Suites,
				Components: gc.Components,
				Operations: gc.Operations,
			})
		}
		grants[token.Identity] = grantList
	}
	return NewStaticAuthorizer(grants)
}
