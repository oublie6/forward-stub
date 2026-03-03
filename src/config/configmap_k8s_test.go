package config

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestK8sConfigMapConfigJSONIsValid(t *testing.T) {
	raw, err := loadK8sConfigMapConfigJSON("../../deploy/k8s/configmap.yaml")
	if err != nil {
		t.Fatalf("load k8s configmap config.json failed: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("config.json in k8s configmap is invalid JSON: %v", err)
	}

	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config.json in k8s configmap is semantically invalid: %v", err)
	}
}

func loadK8sConfigMapConfigJSON(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(b), "\n")

	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "config.json: |" {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return "", errors.New("config.json block not found")
	}

	var body []string
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			body = append(body, "")
			continue
		}
		if strings.HasPrefix(line, "    ") {
			body = append(body, strings.TrimPrefix(line, "    "))
			continue
		}
		break
	}
	if len(body) == 0 {
		return "", errors.New("config.json block is empty")
	}

	return strings.Join(body, "\n"), nil
}
