package reconcile

import (
	"fmt"
	"os"
	"strconv"
)

type EnvConfig struct {
	NamespaceMode string
	BaseNamespace string
	RouteTemplate string
	ImageTemplate string
	TagStrategy   string
	DefaultPort   int32
}

func LoadEnvConfig() (EnvConfig, error) {
	cfg := EnvConfig{
		NamespaceMode: os.Getenv("PREVIEW_NAMESPACE_MODE"),
		BaseNamespace: os.Getenv("PREVIEW_BASE_NAMESPACE"),
		RouteTemplate: os.Getenv("ROUTE_DOMAIN_TEMPLATE"),
		ImageTemplate: os.Getenv("IMAGE_REF_TEMPLATE"),
		TagStrategy:   os.Getenv("IMAGE_TAG_STRATEGY"),
		DefaultPort:   8080,
	}

	if cfg.NamespaceMode == "" {
		return EnvConfig{}, fmt.Errorf("PREVIEW_NAMESPACE_MODE is required")
	}
	if cfg.RouteTemplate == "" {
		return EnvConfig{}, fmt.Errorf("ROUTE_DOMAIN_TEMPLATE is required")
	}
	if cfg.ImageTemplate == "" {
		return EnvConfig{}, fmt.Errorf("IMAGE_REF_TEMPLATE is required")
	}
	if cfg.TagStrategy == "" {
		return EnvConfig{}, fmt.Errorf("IMAGE_TAG_STRATEGY is required")
	}

	if rawPort := os.Getenv("DEFAULT_CONTAINER_PORT"); rawPort != "" {
		parsed, err := strconv.Atoi(rawPort)
		if err != nil {
			return EnvConfig{}, fmt.Errorf("DEFAULT_CONTAINER_PORT invalid: %w", err)
		}
		cfg.DefaultPort = int32(parsed)
	}

	return cfg, nil
}
