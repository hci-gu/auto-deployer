package github

import "strings"

func ParseAllowedOrgs(raw string) map[string]struct{} {
	orgs := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		org := strings.TrimSpace(part)
		if org == "" {
			continue
		}
		orgs[strings.ToLower(org)] = struct{}{}
	}
	return orgs
}

func OrgAllowed(allowed map[string]struct{}, repoFullName string) bool {
	if len(allowed) == 0 {
		return true
	}
	parts := strings.SplitN(repoFullName, "/", 3)
	if len(parts) != 2 {
		return false
	}
	org := strings.ToLower(parts[0])
	_, ok := allowed[org]
	return ok
}
