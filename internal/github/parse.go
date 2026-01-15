package github

import (
	"encoding/json"
)

const (
	EventPullRequest = "pull_request"
	EventRepository  = "repository"
)

func ParsePullRequestEvent(body []byte) (PullRequestEvent, error) {
	var event PullRequestEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return PullRequestEvent{}, err
	}
	return event, nil
}

func ParseRepositoryEvent(body []byte) (RepositoryEvent, error) {
	var event RepositoryEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return RepositoryEvent{}, err
	}
	return event, nil
}
