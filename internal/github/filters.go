package github

import (
	"strings"
)

func ParseAllowedRepos(raw string) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, entry := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}
	return allowed
}

func RepoAllowed(allowed map[string]struct{}, repoFullName string) bool {
	if len(allowed) == 0 {
		return false
	}
	_, ok := allowed[repoFullName]
	return ok
}
