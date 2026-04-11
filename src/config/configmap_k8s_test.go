package config

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestK8sConfigMapSplitConfigIsValid(t *testing.T) {
	systemRaw, err := loadK8sConfigMapBlock("../../deploy/k8s/configmap.yaml", "system.json")
	if err != nil {
		t.Fatalf("load k8s configmap system.json failed: %v", err)
	}
	businessRaw, err := loadK8sConfigMapBlock("../../deploy/k8s/configmap.yaml", "business.json")
	if err != nil {
		t.Fatalf("load k8s configmap business.json failed: %v", err)
	}

	var sys SystemConfig
	if err := json.Unmarshal([]byte(systemRaw), &sys); err != nil {
		t.Fatalf("system.json in k8s configmap is invalid JSON: %v", err)
	}
	var biz BusinessConfig
	if err := json.Unmarshal([]byte(businessRaw), &biz); err != nil {
		t.Fatalf("business.json in k8s configmap is invalid JSON: %v", err)
	}

	cfg := sys.Merge(biz)
	cfg.ApplyDefaults(BusinessDefaultsConfig{})
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config merged from k8s configmap is semantically invalid: %v", err)
	}
}

func loadK8sConfigMapBlock(path, block string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(b), "\n")

	start := -1
	marker := block + ": |"
	for i, line := range lines {
		if strings.TrimSpace(line) == marker {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return "", errors.New(block + " block not found")
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
		return "", errors.New(block + " block is empty")
	}

	return strings.Join(body, "\n"), nil
}
