package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultAPIBaseURL = "https://api.github.com"

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(token, baseURL string) *Client {
	if token == "" {
		return nil
	}
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) CreatePRComment(ctx context.Context, repoFullName string, prNumber int, body string) error {
	if c == nil {
		return fmt.Errorf("github client is nil")
	}
	owner, repo, err := splitRepoFullName(repoFullName)
	if err != nil {
		return err
	}
	if prNumber <= 0 {
		return fmt.Errorf("invalid pr number: %d", prNumber)
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("comment body is empty")
	}

	payload := map[string]string{"body": body}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal comment payload: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", c.baseURL, owner, repo, prNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create comment request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "auto-deployer")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send comment request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("comment request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	return nil
}

func splitRepoFullName(fullName string) (string, string, error) {
	parts := strings.SplitN(fullName, "/", 3)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo full name: %s", fullName)
	}
	return parts[0], parts[1], nil
}
