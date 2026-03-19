package config

import "fmt"

// ResolveConfigPaths 解析 system/business 配置路径，支持 legacy 单文件模式。
func ResolveConfigPaths(legacyPath, systemPath, businessPath string) (string, string, error) {
	if systemPath != "" || businessPath != "" {
		if systemPath == "" || businessPath == "" {
			return "", "", fmt.Errorf("必须同时提供 -system-config 和 -business-config")
		}
		return systemPath, businessPath, nil
	}
	if legacyPath == "" {
		return "", "", fmt.Errorf("必须提供 -system-config 和 -business-config，或使用 -config 兼容单文件模式")
	}
	return legacyPath, legacyPath, nil
}

// LoadLocalPair 读取 system/business 本地文件并合并。
func LoadLocalPair(systemPath, businessPath string) (SystemConfig, BusinessConfig, Config, error) {
	sys, err := LoadSystemLocal(systemPath)
	if err != nil {
		return SystemConfig{}, BusinessConfig{}, Config{}, fmt.Errorf("加载系统配置失败: %w", err)
	}
	biz, err := LoadBusinessLocal(businessPath)
	if err != nil {
		return SystemConfig{}, BusinessConfig{}, Config{}, fmt.Errorf("加载业务配置失败: %w", err)
	}
	cfg := sys.Merge(biz)
	return sys, biz, cfg, nil
}
