// local_load.go 负责从本地 JSON 文件读取并反序列化配置。
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// decodeJSONStrict is a package-local helper used by local_load.go.
func decodeJSONStrict(data []byte, v interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err == nil {
		return fmt.Errorf("invalid json: trailing data")
	} else if err != io.EOF {
		return err
	}
	return nil
}

// LoadLocal 负责该函数对应的核心逻辑，详见实现细节。
func LoadLocal(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := decodeJSONStrict(b, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// LoadSystemLocal 负责加载系统配置文件（控制面与日志）。
func LoadSystemLocal(path string) (SystemConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SystemConfig{}, err
	}
	var c SystemConfig
	if err := decodeJSONStrict(b, &c); err != nil {
		return SystemConfig{}, err
	}
	return c, nil
}

// LoadBusinessLocal 负责加载业务配置文件（拓扑与任务）。
func LoadBusinessLocal(path string) (BusinessConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return BusinessConfig{}, err
	}
	var c BusinessConfig
	if err := decodeJSONStrict(b, &c); err != nil {
		return BusinessConfig{}, err
	}
	return c, nil
}
