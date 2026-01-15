package openshift

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (c *Client) ApplyDeployment(ctx context.Context, namespace string, deployment *appsv1.Deployment) (bool, error) {
	client := c.Kube.AppsV1().Deployments(namespace)
	existing, err := client.Get(ctx, deployment.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = client.Create(ctx, deployment, metav1.CreateOptions{})
			return true, err
		}
		return false, err
	}

	deployment.ResourceVersion = existing.ResourceVersion
	_, err = client.Update(ctx, deployment, metav1.UpdateOptions{})
	return false, err
}

func (c *Client) ApplyService(ctx context.Context, namespace string, service *corev1.Service) (bool, error) {
	client := c.Kube.CoreV1().Services(namespace)
	existing, err := client.Get(ctx, service.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = client.Create(ctx, service, metav1.CreateOptions{})
			return true, err
		}
		return false, err
	}

	service.ResourceVersion = existing.ResourceVersion
	preserveServiceFields(existing, service)
	_, err = client.Update(ctx, service, metav1.UpdateOptions{})
	return false, err
}

func (c *Client) ApplyRoute(ctx context.Context, namespace string, route *unstructured.Unstructured) (bool, error) {
	client := c.Dynamic.Resource(RouteGVR).Namespace(namespace)
	existing, err := client.Get(ctx, route.GetName(), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = client.Create(ctx, route, metav1.CreateOptions{})
			return true, err
		}
		return false, err
	}

	route.SetResourceVersion(existing.GetResourceVersion())
	_, err = client.Update(ctx, route, metav1.UpdateOptions{})
	return false, err
}

func preserveServiceFields(existing *corev1.Service, desired *corev1.Service) {
	if desired.Spec.ClusterIP == "" {
		desired.Spec.ClusterIP = existing.Spec.ClusterIP
	}
	if len(desired.Spec.ClusterIPs) == 0 && len(existing.Spec.ClusterIPs) > 0 {
		desired.Spec.ClusterIPs = existing.Spec.ClusterIPs
	}
	if desired.Spec.IPFamilies == nil && existing.Spec.IPFamilies != nil {
		desired.Spec.IPFamilies = existing.Spec.IPFamilies
	}
	if desired.Spec.IPFamilyPolicy == nil && existing.Spec.IPFamilyPolicy != nil {
		desired.Spec.IPFamilyPolicy = existing.Spec.IPFamilyPolicy
	}
	for i := range desired.Spec.Ports {
		desired.Spec.Ports[i].NodePort = existingNodePort(existing, desired.Spec.Ports[i].Name)
	}
}

func existingNodePort(existing *corev1.Service, name string) int32 {
	for _, port := range existing.Spec.Ports {
		if port.Name == name {
			return port.NodePort
		}
	}
	return 0
}
