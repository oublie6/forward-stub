// validate.go 校验配置完整性、参数合法性与引用关系。
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Validate 校验完整 Config 的语义、引用关系和协议字段联动。
// 调用约定：应先执行 ApplyDefaults，再调用 Validate；这样依赖默认值的字段
// （例如 Kafka balancers、OSS part_size、SkyDDS drain 参数）会以最终生效值参与校验。
func (c *Config) Validate() error {
	if len(c.Tasks) == 0 {
		return errors.New("no tasks")
	}
	if c.Receivers == nil {
		return errors.New("receivers missing")
	}
	if c.Selectors == nil {
		return errors.New("selectors missing")
	}
	if c.TaskSets == nil {
		return errors.New("task_sets missing")
	}
	if c.Senders == nil {
		return errors.New("senders missing")
	}
	if c.Pipelines == nil {
		return errors.New("pipelines missing")
	}

	if strings.TrimSpace(c.Logging.GCStatsLogInterval) != "" {
		d, err := time.ParseDuration(c.Logging.GCStatsLogInterval)
		if err != nil || d <= 0 {
			return errors.New("logging gc_stats_log_interval must be a valid duration > 0")
		}
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

	for tsn, taskNames := range c.TaskSets {
		if tsn == "" {
			return errors.New("task set name empty")
		}
		if len(taskNames) == 0 {
			return fmt.Errorf("task set %s has no tasks", tsn)
		}
		for _, tn := range taskNames {
			if tn == "" {
				return fmt.Errorf("task set %s has empty task name", tsn)
			}
			if _, ok := c.Tasks[tn]; !ok {
				return fmt.Errorf("task set %s task %s not found", tsn, tn)
			}
		}
	}

	for sn, sc := range c.Selectors {
		if sn == "" {
			return errors.New("selector name empty")
		}
		for key, taskSetName := range sc.Matches {
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("selector %s has empty match key", sn)
			}
			if strings.TrimSpace(taskSetName) == "" {
				return fmt.Errorf("selector %s match key %s has empty task set", sn, key)
			}
			if _, ok := c.TaskSets[taskSetName]; !ok {
				return fmt.Errorf("selector %s task set %s not found", sn, taskSetName)
			}
		}
		if sc.DefaultTaskSet != "" {
			if _, ok := c.TaskSets[sc.DefaultTaskSet]; !ok {
				return fmt.Errorf("selector %s default task set %s not found", sn, sc.DefaultTaskSet)
			}
		}
	}

	for rn, r := range c.Receivers {
		if strings.TrimSpace(r.Selector) == "" {
			return fmt.Errorf("receiver %s selector required", rn)
		}
		if _, ok := c.Selectors[r.Selector]; !ok {
			return fmt.Errorf("receiver %s selector %s not found", rn, r.Selector)
		}
		if err := validateReceiverMatchKey(rn, r); err != nil {
			return err
		}
		switch r.Type {
		case "udp_gnet", "tcp_gnet":
		case "local_timer":
			if err := validateLocalTimerReceiver(rn, r); err != nil {
				return err
			}
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
			if err := validateKafkaReceiverOptions(rn, r); err != nil {
				return err
			}
			if err := validateKafkaAuth("receiver", rn, r.SASLMechanism, r.Username, r.Password); err != nil {
				return err
			}
		case "dds_skydds":
			if strings.TrimSpace(r.DCPSConfigFile) == "" {
				return fmt.Errorf("receiver %s dds_skydds requires dcps_config_file", rn)
			}
			if r.DomainID < 0 {
				return fmt.Errorf("receiver %s dds_skydds domain_id must be >= 0", rn)
			}
			if strings.TrimSpace(r.TopicName) == "" {
				return fmt.Errorf("receiver %s dds_skydds requires topic_name", rn)
			}
			model := strings.ToLower(strings.TrimSpace(r.MessageModel))
			if model != "octet" && model != "batch_octet" {
				return fmt.Errorf("receiver %s dds_skydds message_model must be octet or batch_octet", rn)
			}
			if err := validatePositiveDurationByType("receiver", rn, "dds_skydds", "wait_timeout", r.WaitTimeout); err != nil {
				return err
			}
			if r.DrainMaxItems <= 0 {
				return fmt.Errorf("receiver %s dds_skydds drain_max_items must be > 0", rn)
			}
			if r.DrainBufferBytes <= 0 {
				return fmt.Errorf("receiver %s dds_skydds drain_buffer_bytes must be > 0", rn)
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
		case "oss":
			if strings.TrimSpace(r.Endpoint) == "" {
				return fmt.Errorf("receiver %s oss requires endpoint", rn)
			}
			if strings.TrimSpace(r.Bucket) == "" {
				return fmt.Errorf("receiver %s oss requires bucket", rn)
			}
			if strings.TrimSpace(r.AccessKey) == "" || strings.TrimSpace(r.SecretKey) == "" {
				return fmt.Errorf("receiver %s oss requires access_key and secret_key", rn)
			}
			if r.ChunkSize < 0 {
				return fmt.Errorf("receiver %s oss chunk_size must be >= 0", rn)
			}
		default:
			return fmt.Errorf("receiver %s unknown type %s", rn, r.Type)
		}
	}

	for sn, s := range c.Senders {
		if err := validateSenderConcurrency(sn, s.Concurrency); err != nil {
			return err
		}
		if err := validateNotifyOnSuccess(sn, s.NotifyOnSuccess); err != nil {
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
			if err := validateKafkaSenderOptions(sn, s); err != nil {
				return err
			}
			if err := validateKafkaAuth("sender", sn, s.SASLMechanism, s.Username, s.Password); err != nil {
				return err
			}
		case "dds_skydds":
			if strings.TrimSpace(s.DCPSConfigFile) == "" {
				return fmt.Errorf("sender %s dds_skydds requires dcps_config_file", sn)
			}
			if s.DomainID < 0 {
				return fmt.Errorf("sender %s dds_skydds domain_id must be >= 0", sn)
			}
			if strings.TrimSpace(s.TopicName) == "" {
				return fmt.Errorf("sender %s dds_skydds requires topic_name", sn)
			}
			model := strings.ToLower(strings.TrimSpace(s.MessageModel))
			if model != "octet" && model != "batch_octet" {
				return fmt.Errorf("sender %s dds_skydds message_model must be octet or batch_octet", sn)
			}
			if model == "batch_octet" {
				if s.BatchNum <= 0 {
					return fmt.Errorf("sender %s dds_skydds batch_num must be > 0 when message_model=batch_octet", sn)
				}
				if s.BatchSize <= 0 {
					return fmt.Errorf("sender %s dds_skydds batch_size must be > 0 when message_model=batch_octet", sn)
				}
				if err := validatePositiveDurationField("sender", sn, "batch_delay", s.BatchDelay); err != nil {
					return err
				}
			} else {
				if s.BatchNum != 0 || s.BatchSize != 0 || strings.TrimSpace(s.BatchDelay) != "" {
					return fmt.Errorf("sender %s dds_skydds batch_* only allowed when message_model=batch_octet", sn)
				}
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
		case "oss":
			if strings.TrimSpace(s.Endpoint) == "" {
				return fmt.Errorf("sender %s oss requires endpoint", sn)
			}
			if strings.TrimSpace(s.Bucket) == "" {
				return fmt.Errorf("sender %s oss requires bucket", sn)
			}
			if strings.TrimSpace(s.AccessKey) == "" || strings.TrimSpace(s.SecretKey) == "" {
				return fmt.Errorf("sender %s oss requires access_key and secret_key", sn)
			}
			if s.PartSize <= 0 {
				return fmt.Errorf("sender %s oss part_size must be > 0", sn)
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

// ErrUnsupportedReceiverMatchKeyMode 返回统一的 receiver match key 模式非法错误。
func ErrUnsupportedReceiverMatchKeyMode(receiverType, mode string) error {
	return fmt.Errorf("%s receiver match_key.mode 不支持: %s", receiverType, mode)
}

func validateReceiverMatchKey(name string, rc ReceiverConfig) error {
	mode := strings.TrimSpace(rc.MatchKey.Mode)
	fixedValue := rc.MatchKey.FixedValue
	switch rc.Type {
	case "udp_gnet":
		switch mode {
		case "", "remote_addr", "remote_ip", "local_addr", "local_ip", "fixed":
		default:
			return fmt.Errorf("receiver %s: %w", name, ErrUnsupportedReceiverMatchKeyMode(rc.Type, mode))
		}
	case "tcp_gnet":
		switch mode {
		case "", "remote_addr", "remote_ip", "local_addr", "local_port", "fixed":
		default:
			return fmt.Errorf("receiver %s: %w", name, ErrUnsupportedReceiverMatchKeyMode(rc.Type, mode))
		}
	case "kafka":
		switch mode {
		case "", "topic", "topic_partition", "fixed":
		default:
			return fmt.Errorf("receiver %s: %w", name, ErrUnsupportedReceiverMatchKeyMode(rc.Type, mode))
		}
	case "sftp":
		switch mode {
		case "", "remote_path", "filename", "fixed":
		default:
			return fmt.Errorf("receiver %s: %w", name, ErrUnsupportedReceiverMatchKeyMode(rc.Type, mode))
		}
	case "oss":
		switch mode {
		case "", "remote_path", "filename", "fixed":
		default:
			return fmt.Errorf("receiver %s: %w", name, ErrUnsupportedReceiverMatchKeyMode(rc.Type, mode))
		}
	case "dds_skydds":
		switch mode {
		case "", "fixed":
		default:
			return fmt.Errorf("receiver %s: %w", name, ErrUnsupportedReceiverMatchKeyMode(rc.Type, mode))
		}
	case "local_timer":
		switch mode {
		case "", "fixed":
		default:
			return fmt.Errorf("receiver %s: %w", name, ErrUnsupportedReceiverMatchKeyMode(rc.Type, mode))
		}
	default:
		return fmt.Errorf("receiver %s unknown type %s", name, rc.Type)
	}
	if mode == "fixed" && strings.TrimSpace(fixedValue) == "" {
		return fmt.Errorf("receiver %s %s match_key.fixed_value 不能为空", name, rc.Type)
	}
	if mode != "fixed" && strings.TrimSpace(fixedValue) != "" {
		return fmt.Errorf("receiver %s %s match_key.fixed_value 仅可在 mode=fixed 时配置", name, rc.Type)
	}
	return nil
}

func validateNotifyOnSuccess(senderName string, notifiers NotifyOnSuccessConfigs) error {
	for i, nc := range notifiers {
		prefix := fmt.Sprintf("sender %s notify_on_success[%d]", senderName, i)
		switch strings.TrimSpace(nc.Type) {
		case "kafka":
			if strings.TrimSpace(nc.Remote) == "" {
				return fmt.Errorf("%s kafka requires remote", prefix)
			}
			if strings.TrimSpace(nc.Topic) == "" {
				return fmt.Errorf("%s kafka requires topic", prefix)
			}
			if err := validateKafkaAuth("notify_on_success", senderName, nc.SASLMechanism, nc.Username, nc.Password); err != nil {
				return err
			}
			if err := validatePositiveDurationByType("sender", senderName, "notify_on_success.kafka", "dial_timeout", nc.DialTimeout); err != nil {
				return err
			}
			if err := validatePositiveDurationByType("sender", senderName, "notify_on_success.kafka", "request_timeout", nc.RequestTimeout); err != nil {
				return err
			}
			if err := validatePositiveDurationByType("sender", senderName, "notify_on_success.kafka", "retry_timeout", nc.RetryTimeout); err != nil {
				return err
			}
			if err := validatePositiveDurationByType("sender", senderName, "notify_on_success.kafka", "retry_backoff", nc.RetryBackoff); err != nil {
				return err
			}
			if err := validatePositiveDurationByType("sender", senderName, "notify_on_success.kafka", "metadata_max_age", nc.MetadataMaxAge); err != nil {
				return err
			}
			if err := validatePositiveDurationByType("sender", senderName, "notify_on_success.kafka", "conn_idle_timeout", nc.ConnIdleTimeout); err != nil {
				return err
			}
			switch strings.TrimSpace(nc.RecordKeySource) {
			case "", "transfer_id", "file_path", "file_name", "sender_name", "receiver_name", "fetch_path":
			default:
				return fmt.Errorf("%s kafka record_key_source unsupported: %s", prefix, nc.RecordKeySource)
			}
		case "dds_skydds":
			if strings.TrimSpace(nc.DCPSConfigFile) == "" {
				return fmt.Errorf("%s dds_skydds requires dcps_config_file", prefix)
			}
			if nc.DomainID < 0 {
				return fmt.Errorf("%s dds_skydds domain_id must be >= 0", prefix)
			}
			if strings.TrimSpace(nc.TopicName) == "" {
				return fmt.Errorf("%s dds_skydds requires topic_name", prefix)
			}
			if strings.ToLower(strings.TrimSpace(nc.MessageModel)) != "octet" {
				return fmt.Errorf("%s dds_skydds message_model only supports octet in this version", prefix)
			}
		case "":
			return fmt.Errorf("%s type required", prefix)
		default:
			return fmt.Errorf("%s unsupported type %s", prefix, nc.Type)
		}
	}
	return nil
}

func validateLocalTimerReceiver(name string, rc ReceiverConfig) error {
	g := rc.Generator
	if strings.TrimSpace(g.PayloadData) == "" {
		return fmt.Errorf("receiver %s local_timer generator payload_data required", name)
	}
	switch strings.TrimSpace(g.PayloadFormat) {
	case "hex", "text", "base64":
	default:
		return fmt.Errorf("receiver %s local_timer generator payload_format must be one of hex/text/base64", name)
	}
	if g.RatePerSec < 0 {
		return fmt.Errorf("receiver %s local_timer generator rate_per_sec must be > 0", name)
	}
	hasInterval := strings.TrimSpace(g.Interval) != ""
	hasRate := g.RatePerSec > 0
	if hasInterval == hasRate {
		return fmt.Errorf("receiver %s local_timer generator interval and rate_per_sec must be configured exactly one", name)
	}
	if hasInterval {
		if err := validatePositiveDurationByType("receiver", name, "local_timer", "generator.interval", g.Interval); err != nil {
			return err
		}
	}
	if strings.TrimSpace(g.TickInterval) != "" {
		if err := validatePositiveDurationByType("receiver", name, "local_timer", "generator.tick_interval", g.TickInterval); err != nil {
			return err
		}
	}
	if strings.TrimSpace(g.StartDelay) != "" {
		if err := validatePositiveDurationByType("receiver", name, "local_timer", "generator.start_delay", g.StartDelay); err != nil {
			return err
		}
	}
	if g.TotalPackets < 0 {
		return fmt.Errorf("receiver %s local_timer generator total_packets must be >= 0", name)
	}
	return nil
}

func validateKafkaReceiverOptions(name string, rc ReceiverConfig) error {
	if err := validatePositiveDurationField("receiver", name, "dial_timeout", rc.DialTimeout); err != nil {
		return err
	}
	if err := validatePositiveDurationField("receiver", name, "conn_idle_timeout", rc.ConnIdleTimeout); err != nil {
		return err
	}
	if err := validatePositiveDurationField("receiver", name, "metadata_max_age", rc.MetadataMaxAge); err != nil {
		return err
	}
	if err := validatePositiveDurationField("receiver", name, "retry_backoff", rc.RetryBackoff); err != nil {
		return err
	}
	if err := validatePositiveDurationField("receiver", name, "session_timeout", rc.SessionTimeout); err != nil {
		return err
	}
	if err := validatePositiveDurationField("receiver", name, "heartbeat_interval", rc.HeartbeatInterval); err != nil {
		return err
	}
	if err := validatePositiveDurationField("receiver", name, "rebalance_timeout", rc.RebalanceTimeout); err != nil {
		return err
	}
	if err := validatePositiveDurationField("receiver", name, "auto_commit_interval", rc.AutoCommitInterval); err != nil {
		return err
	}
	if rc.FetchMaxPartitionBytes < 0 {
		return fmt.Errorf("receiver %s kafka fetch_max_partition_bytes must be >= 0", name)
	}
	if rc.FetchMaxBytes > 0 && rc.FetchMaxPartitionBytes > 0 && rc.FetchMaxPartitionBytes > rc.FetchMaxBytes {
		return fmt.Errorf("receiver %s kafka fetch_max_partition_bytes must be <= fetch_max_bytes", name)
	}
	if rc.IsolationLevel != "" && rc.IsolationLevel != "read_uncommitted" && rc.IsolationLevel != "read_committed" {
		return fmt.Errorf("receiver %s kafka isolation_level unsupported: %s", name, rc.IsolationLevel)
	}
	if len(rc.Balancers) == 0 {
		return fmt.Errorf("receiver %s kafka balancers must not be empty", name)
	}
	for _, balancer := range rc.Balancers {
		switch balancer {
		case "range", "round_robin", "cooperative_sticky":
		default:
			return fmt.Errorf("receiver %s kafka balancer unsupported: %s", name, balancer)
		}
	}
	if rc.SessionTimeout != "" && rc.HeartbeatInterval != "" {
		sessionTimeout, _ := time.ParseDuration(rc.SessionTimeout)
		heartbeatInterval, _ := time.ParseDuration(rc.HeartbeatInterval)
		if heartbeatInterval >= sessionTimeout {
			return fmt.Errorf("receiver %s kafka heartbeat_interval must be less than session_timeout", name)
		}
	}
	autoCommit := true
	if rc.AutoCommit != nil {
		autoCommit = *rc.AutoCommit
	}
	if !autoCommit && strings.TrimSpace(rc.AutoCommitInterval) != "" {
		return fmt.Errorf("receiver %s kafka auto_commit_interval requires auto_commit=true", name)
	}
	return nil
}

func validateKafkaSenderOptions(name string, sc SenderConfig) error {
	if err := validatePositiveDurationField("sender", name, "dial_timeout", sc.DialTimeout); err != nil {
		return err
	}
	if err := validatePositiveDurationField("sender", name, "request_timeout", sc.RequestTimeout); err != nil {
		return err
	}
	if err := validatePositiveDurationField("sender", name, "retry_timeout", sc.RetryTimeout); err != nil {
		return err
	}
	if err := validatePositiveDurationField("sender", name, "retry_backoff", sc.RetryBackoff); err != nil {
		return err
	}
	if err := validatePositiveDurationField("sender", name, "conn_idle_timeout", sc.ConnIdleTimeout); err != nil {
		return err
	}
	if err := validatePositiveDurationField("sender", name, "metadata_max_age", sc.MetadataMaxAge); err != nil {
		return err
	}
	if sc.Partitioner != "" && sc.Partitioner != "sticky" && sc.Partitioner != "round_robin" && sc.Partitioner != "hash_key" {
		return fmt.Errorf("sender %s kafka partitioner unsupported: %s", name, sc.Partitioner)
	}
	if sc.RecordKey != "" && sc.RecordKeySource != "" {
		return fmt.Errorf("sender %s kafka record_key and record_key_source are mutually exclusive", name)
	}
	if sc.RecordKeySource != "" {
		switch sc.RecordKeySource {
		case "payload", "match_key", "remote", "local", "file_name", "file_path", "transfer_id", "route_sender":
		default:
			return fmt.Errorf("sender %s kafka record_key_source unsupported: %s", name, sc.RecordKeySource)
		}
	}
	if sc.Partitioner == "hash_key" && sc.RecordKey == "" && sc.RecordKeySource == "" {
		return fmt.Errorf("sender %s kafka partitioner hash_key requires record_key or record_key_source", name)
	}
	if sc.CompressionLevel != 0 {
		switch sc.Compression {
		case "gzip", "lz4", "zstd":
		default:
			return fmt.Errorf("sender %s kafka compression_level requires compression in gzip/lz4/zstd", name)
		}
	}
	return nil
}

func validatePositiveDurationField(kind, name, field, value string) error {
	return validatePositiveDurationByType(kind, name, "kafka", field, value)
}

func validatePositiveDurationByType(kind, name, typ, field, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return fmt.Errorf("%s %s %s %s must be a valid duration > 0", kind, name, typ, field)
	}
	return nil
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

func validateSenderConcurrency(senderName string, concurrency int) error {
	if concurrency <= 0 {
		return nil
	}
	if concurrency&(concurrency-1) != 0 {
		return fmt.Errorf("sender %s concurrency must be a power of two", senderName)
	}
	return nil
}
