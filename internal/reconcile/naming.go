package reconcile

import (
	"fmt"
	"strings"
)

func ResourcePrefix(app string, prNumber int) string {
	return fmt.Sprintf("%s-pr-%d", app, prNumber)
}

func NamespaceForMode(mode string, base string, app string, prNumber int) (string, error) {
	switch mode {
	case "single":
		if base == "" {
			return "", fmt.Errorf("base namespace required for single mode")
		}
		return base, nil
	case "per-app":
		if base == "" {
			return "", fmt.Errorf("base namespace required for per-app mode")
		}
		return fmt.Sprintf("%s-%s", app, strings.TrimPrefix(base, "-")), nil
	case "per-pr":
		return ResourcePrefix(app, prNumber), nil
	default:
		return "", fmt.Errorf("unknown namespace mode: %s", mode)
	}
}
