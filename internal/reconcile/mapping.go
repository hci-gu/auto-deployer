package reconcile

import (
	"encoding/json"
	"fmt"
	"os"
)

type AppMapping struct {
	AppName       string            `json:"appName"`
	ContainerPort int32             `json:"containerPort"`
	RoutePath     string            `json:"routePath"`
	Env           map[string]string `json:"env"`
}

type MappingFile map[string]AppMapping

func LoadMappingFile(path string) (MappingFile, error) {
	if path == "" {
		return nil, fmt.Errorf("mapping file path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mapping file: %w", err)
	}
	var mapping MappingFile
	if err := json.Unmarshal(data, &mapping); err != nil {
		return nil, fmt.Errorf("parse mapping file: %w", err)
	}
	return mapping, nil
}
