package config

func testSelectors(receiver string, tasks ...string) map[string]SelectorConfig {
	return map[string]SelectorConfig{
		"sel": {
			Receivers: []string{receiver},
			Tasks:     tasks,
		},
	}
}
