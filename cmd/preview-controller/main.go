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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"auto-deployer/internal/build"
	"auto-deployer/internal/config"
	"auto-deployer/internal/github"
	"auto-deployer/internal/openshift"
	"auto-deployer/internal/reconcile"
	"auto-deployer/internal/slack"
)

const (
	defaultAddr = ":8080"
)

type previewJob struct {
	action        string
	previewCfg    reconcile.PreviewConfig
	repoCloneURL  string
	headSHA       string
	buildImages   bool
	buildCfg      build.Config
	namespaceMode string
}

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
	githubAPIBaseURL := os.Getenv("GITHUB_API_BASE_URL")
	githubClient := github.NewClient(githubToken, githubAPIBaseURL)
	if githubToken == "" {
		logger.Info("GITHUB_TOKEN is empty; PR comments are disabled")
	}

	allowedReposRaw := os.Getenv("GITHUB_ALLOWED_REPOS")
	allowedRepos := github.ParseAllowedRepos(allowedReposRaw)
	if len(allowedRepos) == 0 {
		logger.Warn("GITHUB_ALLOWED_REPOS is empty; all pull_request webhook requests will be rejected")
	}

	repoEventsEnabled := os.Getenv("GITHUB_REPO_EVENTS_ENABLED") == "true"
	repoEventsAllowedOrgs := github.ParseAllowedOrgs(os.Getenv("GITHUB_REPO_EVENTS_ALLOWED_ORGS"))
	if repoEventsEnabled && len(repoEventsAllowedOrgs) == 0 {
		logger.Warn("GITHUB_REPO_EVENTS_ALLOWED_ORGS is empty; repository events will be rejected")
	}

	rejectForks := os.Getenv("GITHUB_REJECT_FORKS") == "true"
	keepOnMerge := os.Getenv("KEEP_ON_MERGE") == "true"
	buildImages := os.Getenv("IMAGE_BUILD_ENABLED") == "true"

	slackClient := slack.NewClient(
		os.Getenv("SLACK_WEBHOOK_URL"),
		os.Getenv("SLACK_BOT_TOKEN"),
		os.Getenv("SLACK_CHANNEL_ID"),
	)
	dockerfilePath := os.Getenv("IMAGE_BUILD_DOCKERFILE")
	buildPlatform := os.Getenv("IMAGE_BUILD_PLATFORM")
	useBuildx := true
	if raw := os.Getenv("IMAGE_BUILD_USE_BUILDX"); raw != "" {
		useBuildx = raw == "true"
	}

	mappingPath := os.Getenv("APP_MAPPING_FILE")
	if mappingPath == "" {
		mappingPath = "config/app-mapping.json"
	}
	mapping, err := reconcile.LoadMappingFile(mappingPath)
	if err != nil {
		logger.Error("failed to load app mapping", "error", err)
		return
	}

	envConfig, err := reconcile.LoadEnvConfig()
	if err != nil {
		logger.Error("invalid environment config", "error", err)
		return
	}

	client, err := openshift.NewClientFromEnv()
	if err != nil {
		logger.Error("failed to create OpenShift client", "error", err)
		return
	}

	staleCleanupEnabled := true
	if raw := os.Getenv("STALE_CLEANUP_ENABLED"); raw != "" {
		staleCleanupEnabled = raw == "true"
	}

	staleMaxAge := 7 * 24 * time.Hour
	if raw := os.Getenv("STALE_MAX_AGE"); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			staleMaxAge = parsed
		}
	}

	staleCleanupInterval := 24 * time.Hour
	if raw := os.Getenv("STALE_CLEANUP_INTERVAL"); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			staleCleanupInterval = parsed
		}
	}

	queueSize := 20
	if raw := os.Getenv("PREVIEW_QUEUE_SIZE"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			queueSize = parsed
		}
	}
	workerCount := 1
	if raw := os.Getenv("PREVIEW_WORKERS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			workerCount = parsed
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	jobCh := make(chan previewJob, queueSize)
	var workerWG sync.WaitGroup
	startPreviewWorkers(ctx, &workerWG, logger, client, githubClient, jobCh, workerCount)

	if staleCleanupEnabled {
		startStaleCleanupLoop(ctx, logger, client, envConfig.NamespaceMode, staleMaxAge, staleCleanupInterval)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/github", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
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
		switch event {
		case github.EventPullRequest:
			payload, err := github.ParsePullRequestEvent(body)
			if err != nil {
				logger.Error("pull_request parse failed", "error", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			if !github.RepoAllowed(allowedRepos, payload.Repository.FullName) {
				logger.Warn("repo not allowed", "repo", payload.Repository.FullName)
				w.WriteHeader(http.StatusForbidden)
				return
			}

			if rejectForks && payload.PullRequest.Head.Repo.FullName != payload.Repository.FullName {
				logger.Warn("fork pull request rejected",
					"repo", payload.Repository.FullName,
					"head_repo", payload.PullRequest.Head.Repo.FullName,
					"pr", payload.PullRequest.Number,
				)
				w.WriteHeader(http.StatusForbidden)
				return
			}

			appConfig, ok := mapping[payload.Repository.FullName]
			if !ok {
				logger.Warn("no app mapping found", "repo", payload.Repository.FullName)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			port := appConfig.ContainerPort
			if port == 0 {
				port = envConfig.DefaultPort
			}

			tag, err := reconcile.ImageTag(envConfig.TagStrategy, payload.PullRequest.Number, payload.PullRequest.Head.SHA)
			if err != nil {
				logger.Error("image tag render failed", "error", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			imageRef, err := reconcile.RenderTemplate(envConfig.ImageTemplate, appConfig.AppName, tag, payload.PullRequest.Number)
			if err != nil {
				logger.Error("image ref render failed", "error", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			namespace, err := reconcile.NamespaceForMode(envConfig.NamespaceMode, envConfig.BaseNamespace, appConfig.AppName, payload.PullRequest.Number)
			if err != nil {
				logger.Error("namespace render failed", "error", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			routeHost, err := reconcile.RenderTemplate(envConfig.RouteTemplate, appConfig.AppName, tag, payload.PullRequest.Number)
			if err != nil {
				logger.Error("route host render failed", "error", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			previewCfg := reconcile.PreviewConfig{
				AppName:       appConfig.AppName,
				Namespace:     namespace,
				PRNumber:      payload.PullRequest.Number,
				RepoFullName:  payload.Repository.FullName,
				ImageRef:      imageRef,
				ContainerPort: port,
				RouteHost:     routeHost,
				RoutePath:     appConfig.RoutePath,
				HeadSHA:       payload.PullRequest.Head.SHA,
				Env:           appConfig.Env,
			}

			switch payload.Action {
			case "opened", "reopened", "synchronize":
				buildCfg := build.Config{
					Dockerfile: dockerfilePath,
					Platform:   buildPlatform,
					UseBuildx:  useBuildx,
				}
				job := previewJob{
					action:        payload.Action,
					previewCfg:    previewCfg,
					repoCloneURL:  payload.Repository.CloneURL,
					headSHA:       payload.PullRequest.Head.SHA,
					buildImages:   buildImages,
					buildCfg:      buildCfg,
					namespaceMode: envConfig.NamespaceMode,
				}
				if !enqueueJob(logger, jobCh, job) {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
			case "closed":
				if payload.PullRequest.Merged && keepOnMerge {
					logger.Info("preview kept after merge",
						"repo", payload.Repository.FullName,
						"pr", payload.PullRequest.Number,
					)
					w.WriteHeader(http.StatusAccepted)
					return
				}

				job := previewJob{
					action:        payload.Action,
					previewCfg:    previewCfg,
					headSHA:       payload.PullRequest.Head.SHA,
					namespaceMode: envConfig.NamespaceMode,
				}
				if !enqueueJob(logger, jobCh, job) {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
			default:
				logger.Info("pull_request action ignored", "action", payload.Action)
				w.WriteHeader(http.StatusAccepted)
				return
			}
		case github.EventRepository:
			if !repoEventsEnabled {
				logger.Info("repository events disabled")
				w.WriteHeader(http.StatusAccepted)
				return
			}

			payload, err := github.ParseRepositoryEvent(body)
			if err != nil {
				logger.Error("repository parse failed", "error", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			if payload.Action != "created" {
				logger.Info("repository action ignored", "action", payload.Action)
				w.WriteHeader(http.StatusAccepted)
				return
			}

			if !github.OrgAllowed(repoEventsAllowedOrgs, payload.Repository.FullName) {
				logger.Warn("repository org not allowed", "repo", payload.Repository.FullName)
				w.WriteHeader(http.StatusForbidden)
				return
			}

			if slackClient == nil {
				logger.Warn("slack client not configured; cannot notify", "repo", payload.Repository.FullName)
				w.WriteHeader(http.StatusAccepted)
				return
			}

			desc := payload.Repository.Description
			if desc == "" {
				desc = "(no description)"
			}

			msg := "New GitHub repo created: `" + payload.Repository.FullName + "`\n" +
				"URL: " + payload.Repository.HTMLURL + "\n" +
				"Creator: " + payload.Sender.Login + "\n" +
				"Description: " + desc + "\n\n" +
				"Should I add this repo to auto-deployer (`GITHUB_ALLOWED_REPOS` + `config/app-mapping.json`)?"

			notifyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := slackClient.SendMessage(notifyCtx, msg); err != nil {
				logger.Error("slack notify failed", "error", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		default:
			logger.Info("ignored github event", "event", event)
		}

		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// TODO: wire readiness checks for Kubernetes/OpenShift API.
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

	close(jobCh)
	done := make(chan struct{})
	go func() {
		workerWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		logger.Warn("worker shutdown timed out")
	}

	logger.Info("http server stopped")
}

func startPreviewWorkers(ctx context.Context, wg *sync.WaitGroup, logger *slog.Logger, client *openshift.Client, githubClient *github.Client, jobs <-chan previewJob, count int) {
	for i := 0; i < count; i++ {
		workerID := i + 1
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				jobLogger := logger.With(
					"worker", workerID,
					"repo", job.previewCfg.RepoFullName,
					"pr", job.previewCfg.PRNumber,
					"sha", job.headSHA,
				)
				processPreviewJob(ctx, jobLogger, client, githubClient, job)
			}
		}()
	}
}

func startStaleCleanupLoop(ctx context.Context, logger *slog.Logger, client *openshift.Client, namespaceMode string, maxAge, interval time.Duration) {
	logger.Info("stale cleanup enabled",
		"max_age", maxAge.String(),
		"interval", interval.String(),
	)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		run := func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cleanupCancel()

			result, err := reconcile.CleanupStalePreviews(cleanupCtx, client, namespaceMode, maxAge, time.Now().UTC())
			if err != nil {
				logger.Error("stale preview cleanup failed", "error", err)
				return
			}
			logger.Info("stale preview cleanup finished",
				"checked_deployments", result.CheckedDeployments,
				"deleted_previews", result.DeletedPreviews,
				"skipped_deployments", result.SkippedDeployments,
			)
		}

		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			run()
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

func enqueueJob(logger *slog.Logger, jobs chan<- previewJob, job previewJob) bool {
	select {
	case jobs <- job:
		logger.Info("preview job enqueued",
			"repo", job.previewCfg.RepoFullName,
			"pr", job.previewCfg.PRNumber,
			"action", job.action,
		)
		return true
	default:
		logger.Error("preview queue full",
			"repo", job.previewCfg.RepoFullName,
			"pr", job.previewCfg.PRNumber,
			"action", job.action,
		)
		return false
	}
}

func processPreviewJob(ctx context.Context, logger *slog.Logger, client *openshift.Client, githubClient *github.Client, job previewJob) {
	switch job.action {
	case "opened", "reopened", "synchronize":
		if job.buildImages {
			buildCtx, buildCancel := context.WithTimeout(ctx, 20*time.Minute)
			defer buildCancel()

			logger.Info("image build starting", "image", job.previewCfg.ImageRef)
			if err := build.BuildAndPush(buildCtx, job.repoCloneURL, job.headSHA, job.previewCfg.ImageRef, job.buildCfg); err != nil {
				logger.Error("image build failed", "error", err)
				return
			}
			logger.Info("image build finished", "image", job.previewCfg.ImageRef)
		}

		reconcileCtx, reconcileCancel := context.WithTimeout(ctx, 2*time.Minute)
		defer reconcileCancel()
		if err := reconcile.UpsertPreview(reconcileCtx, client, job.previewCfg); err != nil {
			logger.Error("preview reconcile failed", "error", err)
			return
		}
		logger.Info("preview reconciled",
			"namespace", job.previewCfg.Namespace,
			"route", job.previewCfg.RouteHost,
		)
		postPreviewComment(ctx, logger, githubClient, job.previewCfg)
	case "closed":
		deleteCtx, deleteCancel := context.WithTimeout(ctx, 2*time.Minute)
		defer deleteCancel()
		if err := reconcile.DeletePreview(deleteCtx, client, job.previewCfg, job.namespaceMode); err != nil {
			logger.Error("preview delete failed", "error", err)
			return
		}
		logger.Info("preview deleted",
			"namespace", job.previewCfg.Namespace,
		)
	default:
		logger.Info("pull_request action ignored", "action", job.action)
	}
}

func postPreviewComment(ctx context.Context, logger *slog.Logger, githubClient *github.Client, cfg reconcile.PreviewConfig) {
	if githubClient == nil {
		return
	}

	previewURL, err := previewURL(cfg.RouteHost, cfg.RoutePath)
	if err != nil {
		logger.Error("preview URL render failed", "error", err)
		return
	}

	commentBody := fmt.Sprintf("Preview deployment ready: %s", previewURL)
	commentCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := githubClient.CreatePRComment(commentCtx, cfg.RepoFullName, cfg.PRNumber, commentBody); err != nil {
		logger.Error("github comment failed", "error", err)
		return
	}
	logger.Info("github comment posted", "url", previewURL)
}

func previewURL(routeHost, routePath string) (string, error) {
	if strings.TrimSpace(routeHost) == "" {
		return "", fmt.Errorf("route host is empty")
	}

	base := strings.TrimSpace(routeHost)
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}

	path := strings.TrimSpace(routePath)
	if path == "" || path == "/" {
		return base, nil
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return base + path, nil
}
