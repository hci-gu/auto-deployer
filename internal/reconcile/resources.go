package reconcile

import (
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func Labels(cfg PreviewConfig) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     cfg.AppName,
		"app.kubernetes.io/instance": ResourcePrefix(cfg.AppName, cfg.PRNumber),
		"preview-controller/preview": "true",
		"preview-controller/pr":      fmt.Sprintf("%d", cfg.PRNumber),
		"preview-controller/repo":    sanitizeLabelValue(cfg.RepoFullName),
	}
}

func Annotations(cfg PreviewConfig, now time.Time, createdAt string) map[string]string {
	annotations := map[string]string{
		"preview-controller/head-sha":        cfg.HeadSHA,
		"preview-controller/last-updated-at": now.UTC().Format(time.RFC3339),
	}
	if createdAt == "" {
		annotations["preview-controller/created-at"] = now.UTC().Format(time.RFC3339)
	} else {
		annotations["preview-controller/created-at"] = createdAt
	}
	return annotations
}

func BuildDeployment(cfg PreviewConfig, annotations map[string]string) *appsv1.Deployment {
	labels := Labels(cfg)
	name := ResourcePrefix(cfg.AppName, cfg.PRNumber)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   cfg.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  cfg.AppName,
							Image: cfg.ImageRef,
							Ports: []corev1.ContainerPort{{ContainerPort: cfg.ContainerPort}},
							Env:   envVars(cfg.Env),
						},
					},
				},
			},
		},
	}
}

func BuildService(cfg PreviewConfig, annotations map[string]string) *corev1.Service {
	labels := Labels(cfg)
	name := ResourcePrefix(cfg.AppName, cfg.PRNumber)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   cfg.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstrFromInt32(cfg.ContainerPort),
				},
			},
		},
	}
}

func BuildRoute(cfg PreviewConfig, annotations map[string]string) *unstructured.Unstructured {
	labels := Labels(cfg)
	name := ResourcePrefix(cfg.AppName, cfg.PRNumber)

	route := map[string]interface{}{
		"apiVersion": "route.openshift.io/v1",
		"kind":       "Route",
		"metadata": map[string]interface{}{
			"name":        name,
			"namespace":   cfg.Namespace,
			"labels":      labels,
			"annotations": annotations,
		},
		"spec": map[string]interface{}{
			"host": cfg.RouteHost,
			"to": map[string]interface{}{
				"kind": "Service",
				"name": name,
			},
			"port": map[string]interface{}{
				"targetPort": "http",
			},
		},
	}

	if cfg.RoutePath != "" {
		spec := route["spec"].(map[string]interface{})
		spec["path"] = cfg.RoutePath
	}

	return &unstructured.Unstructured{Object: route}
}

func int32Ptr(value int32) *int32 {
	return &value
}

func intstrFromInt32(value int32) intstr.IntOrString {
	return intstr.IntOrString{Type: intstr.Int, IntVal: value}
}

func envVars(values map[string]string) []corev1.EnvVar {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]corev1.EnvVar, 0, len(keys))
	for _, key := range keys {
		env = append(env, corev1.EnvVar{Name: key, Value: values[key]})
	}
	return env
}

func sanitizeLabelValue(value string) string {
	if value == "" {
		return "unknown"
	}
	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		default:
			return '-'
		}
	}, value)
	sanitized = strings.Trim(sanitized, "-_.")
	if sanitized == "" {
		return "unknown"
	}
	return sanitized
}
