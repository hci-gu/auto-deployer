package openshift

import (
	"fmt"
	"os"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Client struct {
	Kube    kubernetes.Interface
	Dynamic dynamic.Interface
}

func NewClientFromEnv() (*Client, error) {
	cfg, err := configFromEnv()
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create kube client: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	return &Client{Kube: kubeClient, Dynamic: dynClient}, nil
}

func configFromEnv() (*rest.Config, error) {
	apiURL := os.Getenv("OPENSHIFT_API_URL")
	if apiURL == "" {
		return rest.InClusterConfig()
	}

	token := os.Getenv("OPENSHIFT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("OPENSHIFT_TOKEN required when OPENSHIFT_API_URL is set")
	}

	caFile := os.Getenv("OPENSHIFT_CA_CERT")
	insecure := os.Getenv("OPENSHIFT_INSECURE_SKIP_TLS_VERIFY") == "true"

	cfg := &rest.Config{
		Host:        apiURL,
		BearerToken: token,
		TLSClientConfig: rest.TLSClientConfig{
			CAFile:   caFile,
			Insecure: insecure,
		},
	}

	return cfg, nil
}
