package config

// testSelectors is a package-local helper used by test_helpers_test.go.
func testSelectors(receiver string, tasks ...string) map[string]SelectorConfig {
	return map[string]SelectorConfig{
		"sel": {
			Receivers: []string{receiver},
			Tasks:     tasks,
		},
	}
}
