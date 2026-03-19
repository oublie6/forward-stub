package config

func attachMinimalRouting(cfg Config) Config {
	if cfg.Receivers == nil {
		cfg.Receivers = map[string]ReceiverConfig{}
	}
	for name, rc := range cfg.Receivers {
		if rc.Selector == "" {
			rc.Selector = "sel1"
			cfg.Receivers[name] = rc
		}
	}
	if cfg.Selectors == nil {
		cfg.Selectors = map[string]SelectorConfig{
			"sel1": {DefaultTaskSet: "ts1"},
		}
	}
	if cfg.TaskSets == nil {
		taskName := "t1"
		for name := range cfg.Tasks {
			taskName = name
			break
		}
		cfg.TaskSets = map[string][]string{
			"ts1": []string{taskName},
		}
	}
	return cfg
}
