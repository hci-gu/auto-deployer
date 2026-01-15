package reconcile

import (
	"fmt"
	"strings"
)

func ImageTag(strategy string, prNumber int, sha string) (string, error) {
	switch strategy {
	case "sha":
		return sha, nil
	case "pr":
		return fmt.Sprintf("pr-%d", prNumber), nil
	case "pr-sha":
		short := sha
		if len(short) > 7 {
			short = sha[:7]
		}
		return fmt.Sprintf("pr-%d-%s", prNumber, short), nil
	default:
		return "", fmt.Errorf("unknown IMAGE_TAG_STRATEGY: %s", strategy)
	}
}

func RenderTemplate(template string, app string, tag string, prNumber int) (string, error) {
	if template == "" {
		return "", fmt.Errorf("template is empty")
	}
	replaced := strings.NewReplacer(
		"{app}", app,
		"{tag}", tag,
		"{pr}", fmt.Sprintf("%d", prNumber),
	).Replace(template)
	return replaced, nil
}
