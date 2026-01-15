package slack

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

const apiBaseURL = "https://slack.com/api"

type Client struct {
	webhookURL string
	botToken   string
	channelID  string
	httpClient *http.Client
}

func NewClient(webhookURL, botToken, channelID string) *Client {
	webhookURL = strings.TrimSpace(webhookURL)
	botToken = strings.TrimSpace(botToken)
	channelID = strings.TrimSpace(channelID)

	if webhookURL == "" && (botToken == "" || channelID == "") {
		return nil
	}

	return &Client{
		webhookURL: webhookURL,
		botToken:   botToken,
		channelID:  channelID,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) SendMessage(ctx context.Context, text string) error {
	if c == nil {
		return fmt.Errorf("slack client is nil")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("text is empty")
	}

	if c.webhookURL != "" {
		return c.sendWebhook(ctx, text)
	}
	return c.sendChatPostMessage(ctx, text)
}

func (c *Client) sendWebhook(ctx context.Context, text string) error {
	payload := map[string]string{"text": text}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("webhook failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	return nil
}

func (c *Client) sendChatPostMessage(ctx context.Context, text string) error {
	if err := c.postMessage(ctx, text); err == nil {
		return nil
	} else if !strings.Contains(err.Error(), "not_in_channel") {
		return err
	}

	// Try to join the channel and retry once. This requires Slack scopes:
	// - public channels: channels:join
	// - private channels: groups:write (or invite the bot manually)
	if err := c.joinChannel(ctx); err != nil {
		return fmt.Errorf("postMessage error: not_in_channel (also failed to join: %w)", err)
	}
	return c.postMessage(ctx, text)
}

func (c *Client) postMessage(ctx context.Context, text string) error {
	payload := map[string]string{
		"channel": c.channelID,
		"text":    text,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal postMessage payload: %w", err)
	}

	url := apiBaseURL + "/chat.postMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create postMessage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send postMessage request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("postMessage failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var parsed struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(bodyBytes, &parsed); err == nil {
		if !parsed.OK {
			return fmt.Errorf("postMessage error: %s", parsed.Error)
		}
	}

	return nil
}

func (c *Client) joinChannel(ctx context.Context) error {
	payload := map[string]string{
		"channel": c.channelID,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal join payload: %w", err)
	}

	url := apiBaseURL + "/conversations.join"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create join request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send join request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("join failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var parsed struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(bodyBytes, &parsed); err == nil {
		if !parsed.OK {
			return fmt.Errorf("join error: %s", parsed.Error)
		}
	}

	return nil
}
