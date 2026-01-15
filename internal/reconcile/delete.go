package reconcile

import (
	"context"
	"fmt"

	"auto-deployer/internal/openshift"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func DeletePreview(ctx context.Context, client *openshift.Client, cfg PreviewConfig, namespaceMode string) error {
	selector := labelSelector(cfg)

	if err := deleteDeployments(ctx, client, cfg.Namespace, selector); err != nil {
		return fmt.Errorf("delete deployments: %w", err)
	}
	if err := deleteServices(ctx, client, cfg.Namespace, selector); err != nil {
		return fmt.Errorf("delete services: %w", err)
	}
	if err := deleteRoutes(ctx, client, cfg.Namespace, selector); err != nil {
		return fmt.Errorf("delete routes: %w", err)
	}

	if namespaceMode == "per-pr" {
		if err := client.Kube.CoreV1().Namespaces().Delete(ctx, cfg.Namespace, metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("delete namespace: %w", err)
		}
	}

	return nil
}

func labelSelector(cfg PreviewConfig) string {
	return fmt.Sprintf("preview-controller/preview=true,preview-controller/pr=%d,preview-controller/repo=%s", cfg.PRNumber, sanitizeLabelValue(cfg.RepoFullName))
}

func NamespaceResource(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func deleteDeployments(ctx context.Context, client *openshift.Client, namespace, selector string) error {
	deployments, err := client.Kube.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	for _, item := range deployments.Items {
		if err := client.Kube.AppsV1().Deployments(namespace).Delete(ctx, item.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func deleteServices(ctx context.Context, client *openshift.Client, namespace, selector string) error {
	services, err := client.Kube.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	for _, item := range services.Items {
		if err := client.Kube.CoreV1().Services(namespace).Delete(ctx, item.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func deleteRoutes(ctx context.Context, client *openshift.Client, namespace, selector string) error {
	routes, err := client.Dynamic.Resource(openshift.RouteGVR).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	for _, item := range routes.Items {
		if err := client.Dynamic.Resource(openshift.RouteGVR).Namespace(namespace).Delete(ctx, item.GetName(), metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	return nil
}
