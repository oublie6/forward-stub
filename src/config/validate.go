// validate.go 校验配置完整性、参数合法性与引用关系。
package config

import (
	"errors"
	"fmt"
)

// Validate 负责该函数对应的核心逻辑，详见实现细节。
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

	for rn, r := range c.Receivers {
		switch r.Type {
		case "udp_gnet", "tcp_gnet":
		case "kafka":
			if r.Listen == "" {
				return fmt.Errorf("receiver %s kafka requires listen as brokers csv", rn)
			}
			if r.Topic == "" {
				return fmt.Errorf("receiver %s kafka requires topic", rn)
			}
		default:
			return fmt.Errorf("receiver %s unknown type %s", rn, r.Type)
		}
	}

	for sn, s := range c.Senders {
		switch s.Type {
		case "udp_unicast", "udp_multicast", "tcp_gnet":
		case "kafka":
			if s.Remote == "" {
				return fmt.Errorf("sender %s kafka requires remote as brokers csv", sn)
			}
			if s.Topic == "" {
				return fmt.Errorf("sender %s kafka requires topic", sn)
			}
		default:
			return fmt.Errorf("sender %s unknown type %s", sn, s.Type)
		}
	}
	return nil
}
