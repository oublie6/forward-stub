// api_client.go 提供远端配置 API 拉取客户端。
package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"forward-stub/src/config"
)

type ConfigAPIClient struct {
	baseURL string
	client  *http.Client
}

// NewConfigAPIClient 负责该函数对应的核心逻辑，详见实现细节。
func NewConfigAPIClient(baseURL string, timeoutSeconds int) *ConfigAPIClient {
	return &ConfigAPIClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
	}
}

// FetchBusinessConfig 从远端 API 拉取业务配置（仅允许 business 维度字段）。
func (c *ConfigAPIClient) FetchBusinessConfig(ctx context.Context) (config.BusinessConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return config.BusinessConfig{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return config.BusinessConfig{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return config.BusinessConfig{}, fmt.Errorf("配置中心返回非成功状态: %s", resp.Status)
	}

	var cfg config.BusinessConfig
	dec := json.NewDecoder(resp.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return config.BusinessConfig{}, err
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err == nil {
		return config.BusinessConfig{}, fmt.Errorf("JSON格式非法: 存在多余内容")
	} else if err != io.EOF {
		return config.BusinessConfig{}, err
	}
	return cfg, nil
}
