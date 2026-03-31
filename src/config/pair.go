package config

import "fmt"

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
