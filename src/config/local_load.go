// local_load.go 负责从本地 JSON 文件读取并反序列化配置。
package config

import (
	"encoding/json"
	"os"
)

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
