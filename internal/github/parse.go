package github

import (
	"encoding/json"
)

const (
	EventPullRequest = "pull_request"
)

func ParsePullRequestEvent(body []byte) (PullRequestEvent, error) {
	var event PullRequestEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return PullRequestEvent{}, err
	}
	return event, nil
}
