package config

// testSelectors 是供 test_helpers_test.go 使用的包内辅助函数。
func testSelectors(receiver string, tasks ...string) map[string]SelectorConfig {
	return map[string]SelectorConfig{
		"sel": {
			Receivers: []string{receiver},
			Tasks:     tasks,
		},
	}
}
