// validate.go 校验配置完整性、参数合法性与引用关系。
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Validate 负责该函数对应的核心逻辑，详见实现细节。
func (c *Config) Validate() error {
	if len(c.Tasks) == 0 {
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

	if c.Logging.PayloadPoolMaxCachedBytes < 0 {
		return errors.New("logging payload_pool_max_cached_bytes must be >= 0")
	}

	if c.Control.PprofPort < -1 || c.Control.PprofPort > 65535 {
		return errors.New("control pprof_port must be in [-1,65535]")
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
		if t.ChannelQueueSize < 0 {
			return fmt.Errorf("task %s channel_queue_size must be >= 0", tn)
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
		for _, pn := range t.Pipelines {
			stages := c.Pipelines[pn]
			for _, sc := range stages {
				if sc.Type != "route_offset_bytes_sender" {
					continue
				}
				for _, target := range routeStageTargets(sc) {
					if !containsString(t.Senders, target) {
						return fmt.Errorf("task %s pipeline %s route target sender %s not in task senders", tn, pn, target)
					}
				}
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
			if r.HostKeyFingerprint == "" {
				return fmt.Errorf("receiver %s sftp requires host_key_fingerprint", rn)
			}
			if err := ValidateSSHHostKeyFingerprint(r.HostKeyFingerprint); err != nil {
				return fmt.Errorf("receiver %s sftp host_key_fingerprint invalid: %w", rn, err)
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
			if s.HostKeyFingerprint == "" {
				return fmt.Errorf("sender %s sftp requires host_key_fingerprint", sn)
			}
			if err := ValidateSSHHostKeyFingerprint(s.HostKeyFingerprint); err != nil {
				return fmt.Errorf("sender %s sftp host_key_fingerprint invalid: %w", sn, err)
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

// ValidateSSHHostKeyFingerprint 校验 SSH SHA256 主机公钥指纹格式。
// 合法格式示例：SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A
func ValidateSSHHostKeyFingerprint(f string) error {
	v := strings.TrimSpace(f)
	if v == "" {
		return errors.New("empty fingerprint")
	}
	const prefix = "SHA256:"
	if !strings.HasPrefix(v, prefix) {
		return fmt.Errorf("must start with %q", prefix)
	}
	raw := strings.TrimSpace(strings.TrimPrefix(v, prefix))
	if raw == "" {
		return errors.New("missing base64 digest")
	}
	digest, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		return fmt.Errorf("invalid base64 digest: %w", err)
	}
	if len(digest) != 32 {
		return fmt.Errorf("invalid digest length: got %d, want 32", len(digest))
	}
	return nil
}

func containsString(items []string, want string) bool {
	for _, it := range items {
		if it == want {
			return true
		}
	}
	return false
}

func routeStageTargets(sc StageConfig) []string {
	targets := make([]string, 0, len(sc.Cases)+1)
	for _, sn := range sc.Cases {
		if sn == "" || containsString(targets, sn) {
			continue
		}
		targets = append(targets, sn)
	}
	if sc.DefaultSender != "" && !containsString(targets, sc.DefaultSender) {
		targets = append(targets, sc.DefaultSender)
	}
	return targets
}
