// local_load.go 负责从本地 JSON 文件读取并反序列化配置。
package config

import (
	"encoding/json"
	"os"
)

// LoadLocal 负责该函数对应的核心逻辑，详见实现细节。
func LoadLocal(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}
