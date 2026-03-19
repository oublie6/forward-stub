// validate.go 校验配置完整性、参数合法性与引用关系。
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"
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
	if c.Selectors == nil {
		return errors.New("selectors missing")
	}

	if c.Logging.PayloadPoolMaxCachedBytes < 0 {
		return errors.New("logging payload_pool_max_cached_bytes must be >= 0")
	}
	if _, err := parsePositiveDuration("logging gc_stats_interval", c.Logging.GCStatsInterval); err != nil {
		return err
	}

	if c.Control.PprofPort < -1 || c.Control.PprofPort > 65535 {
		return errors.New("control pprof_port must be in [-1,65535]")
	}

	for tn, t := range c.Tasks {
		if tn == "" {
			return errors.New("task name empty")
		}
		if len(t.Senders) == 0 {
			return fmt.Errorf("task %s has no senders", tn)
		}
		if t.ExecutionModel != "" && t.ExecutionModel != "fastpath" && t.ExecutionModel != "pool" && t.ExecutionModel != "channel" {
			return fmt.Errorf("task %s unsupported execution_model %q", tn, t.ExecutionModel)
		}
		if t.ChannelQueueSize < 0 {
			return fmt.Errorf("task %s channel_queue_size must be >= 0", tn)
		}
		for _, pn := range t.Pipelines {
			if _, ok := c.Pipelines[pn]; !ok {
				return fmt.Errorf("task %s pipeline %s not found", tn, pn)
			}
		}
		for _, sn := range dedupeStringsPreserveOrder(t.Senders) {
			if _, ok := c.Senders[sn]; !ok {
				return fmt.Errorf("task %s sender %s not found", tn, sn)
			}
		}
		t.Senders = dedupeStringsPreserveOrder(t.Senders)
		t.Pipelines = dedupeStringsPreserveOrder(t.Pipelines)
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
		c.Tasks[tn] = t
	}

	for sn, sc := range c.Selectors {
		if sn == "" {
			return errors.New("selector name empty")
		}
		if err := normalizeAndValidateSelector(c, sn, &sc); err != nil {
			return err
		}
		c.Selectors[sn] = sc
	}
	if err := validateDefaultSelectorUniqueness(c.Selectors); err != nil {
		return err
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
		if err := validateSenderConcurrency(sn, s.Concurrency); err != nil {
			return err
		}
		switch s.Type {
		case "udp_unicast", "udp_multicast", "tcp_gnet":
		case "kafka":
			if s.Remote == "" {
				return fmt.Errorf("sender %s kafka requires remote as brokers csv", sn)
			}
			if s.Topic == "" {
				return fmt.Errorf("sender %s kafka requires topic", sn)
			}
			if !s.Acks.IsValid() {
				return fmt.Errorf("sender %s kafka acks must be one of 0/1/all/-1", sn)
			}
			if s.Compression != "" && s.Compression != "none" && s.Compression != "gzip" && s.Compression != "snappy" && s.Compression != "lz4" && s.Compression != "zstd" {
				return fmt.Errorf("sender %s kafka compression unsupported: %s", sn, s.Compression)
			}
			idempotent := true
			if s.Idempotent != nil {
				idempotent = *s.Idempotent
			}
			acks := s.Acks.Int()
			if idempotent {
				if acks != -1 {
					return fmt.Errorf("sender %s kafka idempotent=true requires acks=all/-1", sn)
				}
			}
			if s.Retries < 0 {
				return fmt.Errorf("sender %s kafka retries must be >= 0", sn)
			}
			if s.MaxInFlightRequestsPerConnection < 0 {
				return fmt.Errorf("sender %s kafka max_in_flight_requests_per_connection must be >= 0", sn)
			}
			if s.MaxBufferedBytes < 0 {
				return fmt.Errorf("sender %s kafka max_buffered_bytes must be >= 0", sn)
			}
			if s.MaxBufferedRecords < 0 {
				return fmt.Errorf("sender %s kafka max_buffered_records must be >= 0", sn)
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

// validateDefaultSelectorUniqueness 是供 validate.go 使用的包内辅助函数。
func validateDefaultSelectorUniqueness(selectors map[string]SelectorConfig) error {
	names := make([]string, 0, len(selectors))
	for name := range selectors {
		names = append(names, name)
	}
	sort.Strings(names)

	defaultsByReceiver := make(map[string]string)
	for _, selectorName := range names {
		sc := selectors[selectorName]
		if sc.Source != nil {
			continue
		}
		receivers := append([]string(nil), sc.Receivers...)
		sort.Strings(receivers)
		for _, receiverName := range receivers {
			if existing, ok := defaultsByReceiver[receiverName]; ok {
				return fmt.Errorf("receiver %s has multiple default selectors: %s, %s", receiverName, existing, selectorName)
			}
			defaultsByReceiver[receiverName] = selectorName
		}
	}
	return nil
}

// normalizeAndValidateSelector 是供 validate.go 使用的包内辅助函数。
func normalizeAndValidateSelector(c *Config, name string, sc *SelectorConfig) error {
	if sc == nil {
		return fmt.Errorf("selector %s config missing", name)
	}
	sc.Receivers = dedupeStringsPreserveOrder(sc.Receivers)
	sc.Tasks = dedupeStringsPreserveOrder(sc.Tasks)
	if len(sc.Receivers) == 0 {
		return fmt.Errorf("selector %s has no receivers", name)
	}
	if len(sc.Tasks) == 0 {
		return fmt.Errorf("selector %s has no tasks", name)
	}
	for _, rn := range sc.Receivers {
		if _, ok := c.Receivers[rn]; !ok {
			return fmt.Errorf("selector %s receiver %s not found", name, rn)
		}
	}
	for _, tn := range sc.Tasks {
		if _, ok := c.Tasks[tn]; !ok {
			return fmt.Errorf("selector %s task %s not found", name, tn)
		}
	}
	if sc.Source == nil {
		return nil
	}
	sc.Source.SrcCIDRs = dedupeStringsPreserveOrder(sc.Source.SrcCIDRs)
	sc.Source.SrcPortRanges = dedupeStringsPreserveOrder(sc.Source.SrcPortRanges)
	if len(sc.Source.SrcCIDRs) == 0 && len(sc.Source.SrcPortRanges) == 0 {
		return fmt.Errorf("selector %s source is empty", name)
	}
	normalizedCIDRs := make([]string, 0, len(sc.Source.SrcCIDRs))
	seenCIDRs := map[string]struct{}{}
	for _, raw := range sc.Source.SrcCIDRs {
		normalized, err := normalizeCIDROrIP(raw)
		if err != nil {
			return fmt.Errorf("selector %s src_cidrs %q invalid: %w", name, raw, err)
		}
		if _, ok := seenCIDRs[normalized]; ok {
			continue
		}
		seenCIDRs[normalized] = struct{}{}
		normalizedCIDRs = append(normalizedCIDRs, normalized)
	}
	normalizedPorts := make([]string, 0, len(sc.Source.SrcPortRanges))
	seenPorts := map[string]struct{}{}
	for _, raw := range sc.Source.SrcPortRanges {
		normalized, err := normalizePortRange(raw)
		if err != nil {
			return fmt.Errorf("selector %s src_port_ranges %q invalid: %w", name, raw, err)
		}
		if _, ok := seenPorts[normalized]; ok {
			continue
		}
		seenPorts[normalized] = struct{}{}
		normalizedPorts = append(normalizedPorts, normalized)
	}
	sort.Strings(normalizedCIDRs)
	sort.Strings(normalizedPorts)
	sc.Source.SrcCIDRs = normalizedCIDRs
	sc.Source.SrcPortRanges = normalizedPorts
	return nil
}

// normalizeCIDROrIP 是供 validate.go 使用的包内辅助函数。
func normalizeCIDROrIP(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", errors.New("empty cidr")
	}
	if strings.Contains(v, "/") {
		prefix, err := netip.ParsePrefix(v)
		if err != nil {
			return "", err
		}
		return prefix.Masked().String(), nil
	}
	addr, err := netip.ParseAddr(v)
	if err != nil {
		return "", err
	}
	bits := addr.BitLen()
	return netip.PrefixFrom(addr, bits).Masked().String(), nil
}

// normalizePortRange 是供 validate.go 使用的包内辅助函数。
func normalizePortRange(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", errors.New("empty port range")
	}
	if !strings.Contains(v, "-") {
		p, err := parsePort(v)
		if err != nil {
			return "", err
		}
		return strconv.Itoa(int(p)), nil
	}
	parts := strings.Split(v, "-")
	if len(parts) != 2 {
		return "", errors.New("must be single port or start-end")
	}
	start, err := parsePort(parts[0])
	if err != nil {
		return "", err
	}
	end, err := parsePort(parts[1])
	if err != nil {
		return "", err
	}
	if start > end {
		return "", errors.New("range start must be <= end")
	}
	if start == end {
		return strconv.Itoa(int(start)), nil
	}
	return fmt.Sprintf("%d-%d", start, end), nil
}

func parsePositiveDuration(field, raw string) (time.Duration, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s invalid: %w", field, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be > 0", field)
	}
	return d, nil
}

// parsePort 是供 validate.go 使用的包内辅助函数。
func parsePort(raw string) (uint16, error) {
	v := strings.TrimSpace(raw)
	p, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	if p < 1 || p > 65535 {
		return 0, errors.New("port must be in [1,65535]")
	}
	return uint16(p), nil
}

// dedupeStringsPreserveOrder 是供 validate.go 使用的包内辅助函数。
func dedupeStringsPreserveOrder(items []string) []string {
	if len(items) <= 1 {
		return items
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

// validateKafkaAuth 是供 validate.go 使用的包内辅助函数。
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

// containsString 是供 validate.go 使用的包内辅助函数。
func containsString(items []string, want string) bool {
	for _, it := range items {
		if it == want {
			return true
		}
	}
	return false
}

// routeStageTargets 是供 validate.go 使用的包内辅助函数。
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

// validateSenderConcurrency 是供 validate.go 使用的包内辅助函数。
func validateSenderConcurrency(senderName string, concurrency int) error {
	if concurrency <= 0 {
		return nil
	}
	if concurrency&(concurrency-1) != 0 {
		return fmt.Errorf("sender %s concurrency must be a power of two", senderName)
	}
	return nil
}
