package reconcile

import (
	"context"
	"fmt"
	"time"

	"auto-deployer/internal/openshift"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func UpsertPreview(ctx context.Context, client *openshift.Client, cfg PreviewConfig) error {
	now := time.Now().UTC()

	createdAt, err := existingCreatedAt(ctx, client, cfg)
	if err != nil {
		return err
	}

	annotations := Annotations(cfg, now, createdAt)
	deployment := BuildDeployment(cfg, annotations)
	service := BuildService(cfg, annotations)
	route := BuildRoute(cfg, annotations)

	if _, err := client.ApplyDeployment(ctx, cfg.Namespace, deployment); err != nil {
		return fmt.Errorf("apply deployment: %w", err)
	}
	if _, err := client.ApplyService(ctx, cfg.Namespace, service); err != nil {
		return fmt.Errorf("apply service: %w", err)
	}
	if _, err := client.ApplyRoute(ctx, cfg.Namespace, route); err != nil {
		return fmt.Errorf("apply route: %w", err)
	}

	return nil
}

func existingCreatedAt(ctx context.Context, client *openshift.Client, cfg PreviewConfig) (string, error) {
	name := ResourcePrefix(cfg.AppName, cfg.PRNumber)
	deployment, err := client.Kube.AppsV1().Deployments(cfg.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	if deployment.Annotations == nil {
		return "", nil
	}
	return deployment.Annotations["preview-controller/created-at"], nil
}
