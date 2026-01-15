package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"auto-deployer/internal/clone"
	"auto-deployer/internal/config"
	"auto-deployer/internal/github"
	"auto-deployer/internal/slack"
)

const (
	defaultAddr      = ":8080"
	defaultCloneRoot = "/tmp/auto-deployer"
	maxWebhookBody   = 2 << 20
	maxContextChars  = 3500
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))

	dotenvPath := os.Getenv("ENV_FILE")
	if dotenvPath == "" {
		dotenvPath = ".env"
	}
	if _, err := os.Stat(dotenvPath); err == nil {
		if err := config.LoadDotenv(dotenvPath, false); err != nil {
			logger.Error("failed to load dotenv file", "path", dotenvPath, "error", err)
			return
		}
		logger.Info("loaded dotenv file", "path", dotenvPath)
	}

	webhookSecret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if webhookSecret == "" {
		logger.Warn("GITHUB_WEBHOOK_SECRET is empty; webhook verification will always fail")
	}

	githubToken := os.Getenv("GITHUB_TOKEN")
	allowedOrgs := github.ParseAllowedOrgs(os.Getenv("GITHUB_ORGS"))

	cloneRoot := strings.TrimSpace(os.Getenv("CLONE_ROOT"))
	if cloneRoot == "" {
		cloneRoot = defaultCloneRoot
	}

	slackClient := slack.NewClient(
		os.Getenv("SLACK_WEBHOOK_URL"),
		os.Getenv("SLACK_BOT_TOKEN"),
		os.Getenv("SLACK_CHANNEL_ID"),
	)
	if slackClient == nil {
		logger.Warn("slack client not configured; notifications disabled")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/github", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
		if err != nil {
			logger.Error("webhook body read failed", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		signature := r.Header.Get("X-Hub-Signature-256")
		if !github.VerifySignature(webhookSecret, body, signature) {
			logger.Warn("invalid webhook signature")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		event := r.Header.Get("X-GitHub-Event")
		if event != github.EventPullRequest {
			logger.Info("ignored github event", "event", event)
			w.WriteHeader(http.StatusAccepted)
			return
		}

		payload, err := github.ParsePullRequestEvent(body)
		if err != nil {
			logger.Error("pull_request parse failed", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if payload.Action != "opened" {
			logger.Info("pull_request action ignored", "action", payload.Action)
			w.WriteHeader(http.StatusAccepted)
			return
		}

		if !github.OrgAllowed(allowedOrgs, payload.Repository.FullName) {
			logger.Warn("repo org not allowed", "repo", payload.Repository.FullName)
			w.WriteHeader(http.StatusForbidden)
			return
		}

		go func(payload github.PullRequestEvent) {
			handlerCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()

			err := handlePullRequestOpened(handlerCtx, logger, slackClient, cloneRoot, githubToken, payload)
			if err != nil {
				logger.Error("pull_request handling failed", "error", err, "repo", payload.Repository.FullName, "pr", payload.PullRequest.Number)
			}
		}(payload)

		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("http server starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown error", "error", err)
		return
	}

	logger.Info("http server stopped")
}

func handlePullRequestOpened(ctx context.Context, logger *slog.Logger, slackClient *slack.Client, cloneRoot, githubToken string, payload github.PullRequestEvent) error {
	clonePath, err := clone.UniquePath(cloneRoot, payload.Repository.FullName, payload.PullRequest.Number, payload.PullRequest.Head.SHA)
	if err != nil {
		return fmt.Errorf("resolve clone path: %w", err)
	}

	cloneURL, err := clone.WithToken(payload.Repository.CloneURL, githubToken)
	if err != nil {
		return fmt.Errorf("prepare clone url: %w", err)
	}

	if err := clone.CloneRepo(ctx, cloneURL, githubToken, clonePath, payload.PullRequest.Head.SHA); err != nil {
		return fmt.Errorf("clone repo: %w", err)
	}

	message := buildSlackMessage(clonePath, payload)
	if slackClient == nil {
		logger.Warn("slack client not configured; skipping notification", "repo", payload.Repository.FullName, "pr", payload.PullRequest.Number)
		return nil
	}

	notifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := slackClient.SendMessage(notifyCtx, message); err != nil {
		return fmt.Errorf("send slack message: %w", err)
	}

	return nil
}

func buildSlackMessage(clonePath string, payload github.PullRequestEvent) string {
	pr := payload.PullRequest
	repo := payload.Repository

	shortSHA := pr.Head.SHA
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}

	contextText := strings.TrimSpace(pr.Body)
	if contextText == "" {
		contextText = "(no description)"
	}
	contextText = truncate(contextText, maxContextChars)

	instructionsPath := filepath.Join(clonePath, "DEPLOYMENT_INSTRUCTIONS.md")

	lines := []string{
		fmt.Sprintf("PR opened: %s#%d", repo.FullName, pr.Number),
		fmt.Sprintf("URL: %s", pr.URL),
		fmt.Sprintf("Branch: %s", pr.Head.Ref),
		fmt.Sprintf("SHA: %s", shortSHA),
		fmt.Sprintf("Local path: %s", clonePath),
		fmt.Sprintf("Deployment instructions: %s", instructionsPath),
		"",
		"Instructions:",
		"- Check out the folder above.",
		"- Follow DEPLOYMENT_INSTRUCTIONS.md to deploy to OpenShift.",
		"- Decide any repo-specific deployment steps and execute them.",
		"",
		"Additional context (PR title + body):",
		fmt.Sprintf("Title: %s", strings.TrimSpace(pr.Title)),
		"Body:",
		contextText,
	}

	return strings.Join(lines, "\n")
}

func truncate(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return strings.TrimSpace(text[:max]) + "\n…(truncated)"
}
