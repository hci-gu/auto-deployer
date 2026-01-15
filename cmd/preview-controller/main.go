package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"auto-deployer/internal/build"
	"auto-deployer/internal/config"
	"auto-deployer/internal/github"
	"auto-deployer/internal/openshift"
	"auto-deployer/internal/reconcile"
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

	runMode := os.Getenv("RUN_MODE")
	if runMode == "" {
		runMode = "server"
	}

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

	allowedReposRaw := os.Getenv("GITHUB_ALLOWED_REPOS")
	allowedRepos := github.ParseAllowedRepos(allowedReposRaw)
	if len(allowedRepos) == 0 {
		logger.Warn("GITHUB_ALLOWED_REPOS is empty; all webhook requests will be rejected")
	}

	rejectForks := os.Getenv("GITHUB_REJECT_FORKS") == "true"
	keepOnMerge := os.Getenv("KEEP_ON_MERGE") == "true"
	buildImages := os.Getenv("IMAGE_BUILD_ENABLED") == "true"
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

	if runMode == "cleanup" {
		maxAge := 7 * 24 * time.Hour
		if raw := os.Getenv("STALE_MAX_AGE"); raw != "" {
			if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
				maxAge = parsed
			}
		}

		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cleanupCancel()

		result, err := reconcile.CleanupStalePreviews(cleanupCtx, client, envConfig.NamespaceMode, maxAge, time.Now().UTC())
		if err != nil {
			logger.Error("stale preview cleanup failed", "error", err)
			os.Exit(1)
		}
		logger.Info("stale preview cleanup finished",
			"checked_deployments", result.CheckedDeployments,
			"deleted_previews", result.DeletedPreviews,
			"skipped_deployments", result.SkippedDeployments,
		)
		return
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
	startPreviewWorkers(ctx, &workerWG, logger, client, jobCh, workerCount)

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

func startPreviewWorkers(ctx context.Context, wg *sync.WaitGroup, logger *slog.Logger, client *openshift.Client, jobs <-chan previewJob, count int) {
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
				processPreviewJob(ctx, jobLogger, client, job)
			}
		}()
	}
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

func processPreviewJob(ctx context.Context, logger *slog.Logger, client *openshift.Client, job previewJob) {
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
