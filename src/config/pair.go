// pair.go 负责双文件配置模式下的路径解析与本地加载。
package config

import "fmt"

// ResolveConfigPaths 解析 system/business 配置路径，支持 legacy 单文件模式。
func ResolveConfigPaths(legacyPath, systemPath, businessPath string) (string, string, error) {
	if systemPath != "" || businessPath != "" {
		if systemPath == "" || businessPath == "" {
			return "", "", fmt.Errorf("must provide both -system-config and -business-config")
		}
		return systemPath, businessPath, nil
	}
	if legacyPath == "" {
		return "", "", fmt.Errorf("must provide -system-config and -business-config, or use -config as legacy mode")
	}
	return legacyPath, legacyPath, nil
}

// LoadLocalPair 读取 system/business 本地文件并合并。
func LoadLocalPair(systemPath, businessPath string) (SystemConfig, BusinessConfig, Config, error) {
	sys, err := LoadSystemLocal(systemPath)
	if err != nil {
		return SystemConfig{}, BusinessConfig{}, Config{}, fmt.Errorf("load system config error: %w", err)
	}
	biz, err := LoadBusinessLocal(businessPath)
	if err != nil {
		return SystemConfig{}, BusinessConfig{}, Config{}, fmt.Errorf("load business config error: %w", err)
	}
	cfg := sys.Merge(biz)
	return sys, biz, cfg, nil
}
