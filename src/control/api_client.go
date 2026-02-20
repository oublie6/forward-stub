package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"forword-stub/src/config"
)

type ConfigAPIClient struct {
	baseURL string
	client  *http.Client
}

func NewConfigAPIClient(baseURL string, timeoutSeconds int) *ConfigAPIClient {
	return &ConfigAPIClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
	}
}

func (c *ConfigAPIClient) FetchConfig(ctx context.Context) (config.Config, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return config.Config{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return config.Config{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return config.Config{}, fmt.Errorf("config api status=%s", resp.Status)
	}

	var cfg config.Config
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}
