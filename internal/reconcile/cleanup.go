package reconcile

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"auto-deployer/internal/openshift"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	annotationLastUpdatedAt = "preview-controller/last-updated-at"
	annotationCreatedAt     = "preview-controller/created-at"
	labelPreviewEnabled     = "preview-controller/preview"
	labelPRNumber           = "preview-controller/pr"
	labelRepoFullName       = "preview-controller/repo"
)

type CleanupResult struct {
	CheckedDeployments int
	DeletedPreviews    int
	SkippedDeployments int
}

type previewIdentity struct {
	namespace string
	prNumber  int
	repo      string
}

func CleanupStalePreviews(ctx context.Context, client *openshift.Client, namespaceMode string, maxAge time.Duration, now time.Time) (CleanupResult, error) {
	deployments, err := client.Kube.AppsV1().Deployments("").List(ctx, v1.ListOptions{LabelSelector: labelPreviewEnabled + "=true"})
	if err != nil {
		return CleanupResult{}, fmt.Errorf("list deployments: %w", err)
	}

	result := CleanupResult{CheckedDeployments: len(deployments.Items)}
	seen := make(map[previewIdentity]struct{})

	for _, deployment := range deployments.Items {
		if deployment.Labels == nil {
			result.SkippedDeployments++
			continue
		}

		ref, ok := previewIdentityFromLabels(deployment.Namespace, deployment.Labels)
		if !ok {
			result.SkippedDeployments++
			continue
		}

		lastTouched, ok := lastTouchedAt(deployment.Annotations, deployment.CreationTimestamp.Time)
		if !ok {
			result.SkippedDeployments++
			continue
		}

		if now.Sub(lastTouched) <= maxAge {
			continue
		}

		if _, already := seen[ref]; already {
			continue
		}
		seen[ref] = struct{}{}

		cfg := PreviewConfig{
			Namespace:    ref.namespace,
			PRNumber:     ref.prNumber,
			RepoFullName: ref.repo,
		}
		if err := DeletePreview(ctx, client, cfg, namespaceMode); err != nil {
			return result, fmt.Errorf("delete preview %s pr=%d: %w", ref.repo, ref.prNumber, err)
		}
		result.DeletedPreviews++
	}

	return result, nil
}

func previewIdentityFromLabels(namespace string, labels map[string]string) (previewIdentity, bool) {
	if labels[labelPreviewEnabled] != "true" {
		return previewIdentity{}, false
	}

	prRaw, ok := labels[labelPRNumber]
	if !ok {
		return previewIdentity{}, false
	}
	pr, err := strconv.Atoi(prRaw)
	if err != nil || pr <= 0 {
		return previewIdentity{}, false
	}

	repo, ok := labels[labelRepoFullName]
	if !ok || repo == "" {
		return previewIdentity{}, false
	}

	if namespace == "" {
		return previewIdentity{}, false
	}

	return previewIdentity{namespace: namespace, prNumber: pr, repo: repo}, true
}

func lastTouchedAt(annotations map[string]string, fallback time.Time) (time.Time, bool) {
	if annotations != nil {
		if raw := annotations[annotationLastUpdatedAt]; raw != "" {
			if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
				return parsed, true
			}
		}
		if raw := annotations[annotationCreatedAt]; raw != "" {
			if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
				return parsed, true
			}
		}
	}

	if fallback.IsZero() {
		return time.Time{}, false
	}
	return fallback, true
}
