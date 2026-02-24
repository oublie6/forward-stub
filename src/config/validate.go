package config

import (
	"errors"
	"fmt"
)

func (c *Config) Validate() error {
	if c.Tasks == nil || len(c.Tasks) == 0 {
		return errors.New("no tasks")
	}
	if c.Receivers == nil {
		return errors.New("receivers missing")
	}
	if c.Senders == nil {
		return errors.New("senders missing")
	}
	if c.Pipelines == nil {
		return errors.New("pipelines missing")
	}

	for tn, t := range c.Tasks {
		if tn == "" {
			return errors.New("task name empty")
		}
		if len(t.Receivers) == 0 {
			return fmt.Errorf("task %s has no receivers", tn)
		}
		//if len(t.Pipelines) == 0 {
		//	return fmt.Errorf("task %s has no pipelines", tn)
		//}
		if len(t.Senders) == 0 {
			return fmt.Errorf("task %s has no senders", tn)
		}
		for _, rn := range t.Receivers {
			if _, ok := c.Receivers[rn]; !ok {
				return fmt.Errorf("task %s receiver %s not found", tn, rn)
			}
		}
		for _, pn := range t.Pipelines {
			if _, ok := c.Pipelines[pn]; !ok {
				return fmt.Errorf("task %s pipeline %s not found", tn, pn)
			}
		}
		for _, sn := range t.Senders {
			if _, ok := c.Senders[sn]; !ok {
				return fmt.Errorf("task %s sender %s not found", tn, sn)
			}
		}
	}
	return nil
}
