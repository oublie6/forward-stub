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
		if t.ExecutionModel != "" && t.ExecutionModel != "fastpath" && t.ExecutionModel != "pool" && t.ExecutionModel != "channel" {
			return fmt.Errorf("task %s unsupported execution_model %q", tn, t.ExecutionModel)
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
			if r.StartOffset != "" && r.StartOffset != "earliest" && r.StartOffset != "latest" {
				return fmt.Errorf("receiver %s kafka start_offset must be earliest/latest", rn)
			}
			if err := validateKafkaAuth("receiver", rn, r.SASLMechanism, r.Username, r.Password); err != nil {
				return err
			}
		case "sftp":
			if r.Listen == "" {
				return fmt.Errorf("receiver %s sftp requires listen", rn)
			}
			if r.Username == "" || r.Password == "" {
				return fmt.Errorf("receiver %s sftp requires username and password", rn)
			}
			if r.RemoteDir == "" {
				return fmt.Errorf("receiver %s sftp requires remote_dir", rn)
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
			if s.Compression != "" && s.Compression != "none" && s.Compression != "gzip" && s.Compression != "snappy" && s.Compression != "lz4" && s.Compression != "zstd" {
				return fmt.Errorf("sender %s kafka compression unsupported: %s", sn, s.Compression)
			}
			if err := validateKafkaAuth("sender", sn, s.SASLMechanism, s.Username, s.Password); err != nil {
				return err
			}
		case "sftp":
			if s.Remote == "" {
				return fmt.Errorf("sender %s sftp requires remote", sn)
			}
			if s.Username == "" || s.Password == "" {
				return fmt.Errorf("sender %s sftp requires username and password", sn)
			}
			if s.RemoteDir == "" {
				return fmt.Errorf("sender %s sftp requires remote_dir", sn)
			}
		default:
			return fmt.Errorf("sender %s unknown type %s", sn, s.Type)
		}
	}
	return nil
}

func validateKafkaAuth(kind, name, mechanism, username, password string) error {
	if mechanism == "" && username == "" && password == "" {
		return nil
	}
	if username == "" || password == "" {
		return fmt.Errorf("%s %s kafka auth requires both username and password", kind, name)
	}
	if mechanism == "" || mechanism == "PLAIN" || mechanism == "plain" {
		return nil
	}
	return fmt.Errorf("%s %s kafka unsupported sasl_mechanism %s", kind, name, mechanism)
}
