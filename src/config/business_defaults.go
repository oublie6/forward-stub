package config

import "runtime"

// Merge 将系统配置与业务配置拼装为运行时使用的完整 Config。
func (s SystemConfig) Merge(b BusinessConfig) Config {
	cfg := Config{
		Version:   b.Version,
		Control:   s.Control,
		Logging:   s.Logging,
		Receivers: b.Receivers,
		Selectors: b.Selectors,
		TaskSets:  b.TaskSets,
		Senders:   b.Senders,
		Pipelines: b.Pipelines,
		Tasks:     b.Tasks,
	}
	cfg.ApplyBusinessDefaults(s.BusinessDefaults)
	return cfg
}

// ApplyBusinessDefaults 将 system.config 的 business 默认值应用到完整配置。
func (c *Config) ApplyBusinessDefaults(d BusinessDefaultsConfig) {
	if c == nil {
		return
	}
	for name, tc := range c.Tasks {
		if tc.PoolSize <= 0 && d.Task.PoolSize > 0 {
			tc.PoolSize = d.Task.PoolSize
		}
		if tc.ChannelQueueSize <= 0 && d.Task.ChannelQueueSize > 0 {
			tc.ChannelQueueSize = d.Task.ChannelQueueSize
		}
		if tc.ExecutionModel == "" && d.Task.ExecutionModel != "" {
			tc.ExecutionModel = d.Task.ExecutionModel
		}
		if tc.PayloadLogMaxBytes <= 0 && d.Task.PayloadLogMaxBytes > 0 {
			tc.PayloadLogMaxBytes = d.Task.PayloadLogMaxBytes
		}
		c.Tasks[name] = tc
	}
	for name, rc := range c.Receivers {
		if rc.Multicore == nil && d.Receiver.Multicore != nil {
			v := *d.Receiver.Multicore
			rc.Multicore = &v
		}
		if rc.NumEventLoop <= 0 && d.Receiver.NumEventLoop > 0 {
			rc.NumEventLoop = d.Receiver.NumEventLoop
		}
		if rc.PayloadLogMaxBytes <= 0 && d.Receiver.PayloadLogMaxBytes > 0 {
			rc.PayloadLogMaxBytes = d.Receiver.PayloadLogMaxBytes
		}
		c.Receivers[name] = rc
	}
	for name, sc := range c.Senders {
		if sc.Concurrency <= 0 && d.Sender.Concurrency > 0 {
			sc.Concurrency = d.Sender.Concurrency
		}
		c.Senders[name] = sc
	}
}

// ApplyDefaults 为 receiver/task/sender 之外的配置字段填充默认值。
// 用法：在 Validate 之前调用，避免因省略常见参数导致行为不一致。
func (c *Config) ApplyDefaults() {
	if c == nil {
		return
	}
	if c.Control.TimeoutSec <= 0 {
		c.Control.TimeoutSec = DefaultControlTimeoutSec
	}
	if c.Control.ConfigWatchInterval == "" {
		c.Control.ConfigWatchInterval = DefaultConfigWatchInterval
	}
	if c.Control.PprofPort == 0 {
		c.Control.PprofPort = DefaultPprofPort
	}
	if c.Logging.Level == "" {
		c.Logging.Level = DefaultLogLevel
	}
	if c.Logging.MaxSizeMB <= 0 {
		c.Logging.MaxSizeMB = DefaultLogRotateMaxSizeMB
	}
	if c.Logging.MaxBackups <= 0 {
		c.Logging.MaxBackups = DefaultLogRotateMaxBackups
	}
	if c.Logging.MaxAgeDays <= 0 {
		c.Logging.MaxAgeDays = DefaultLogRotateMaxAgeDays
	}
	if c.Logging.Compress == nil {
		v := DefaultLogRotateCompress
		c.Logging.Compress = &v
	}
	if c.Logging.GCStatsLogEnabled == nil {
		v := DefaultGCStatsLogEnabled
		c.Logging.GCStatsLogEnabled = &v
	}
	if c.Logging.GCStatsLogInterval == "" {
		c.Logging.GCStatsLogInterval = DefaultGCStatsLogInterval
	}
	if c.Logging.TrafficStatsInterval == "" {
		c.Logging.TrafficStatsInterval = DefaultTrafficStatsInterval
	}
	if c.Logging.TrafficStatsSampleEvery <= 0 {
		c.Logging.TrafficStatsSampleEvery = DefaultTrafficStatsSampleEvery
	}
	if c.Logging.PayloadLogMaxBytes <= 0 {
		c.Logging.PayloadLogMaxBytes = DefaultPayloadLogMaxBytes
	}
	for name, tc := range c.Tasks {
		if tc.PoolSize <= 0 {
			tc.PoolSize = DefaultTaskPoolSize
		}
		if tc.ChannelQueueSize <= 0 {
			tc.ChannelQueueSize = DefaultTaskChannelQueueSize
		}
		if tc.PayloadLogMaxBytes <= 0 {
			tc.PayloadLogMaxBytes = c.Logging.PayloadLogMaxBytes
		}
		c.Tasks[name] = tc
	}
	for name, rc := range c.Receivers {
		if rc.Multicore == nil {
			v := DefaultReceiverMulticore
			rc.Multicore = &v
		}
		if rc.NumEventLoop <= 0 {
			rc.NumEventLoop = max(DefaultReceiverNumEventLoop, runtime.NumCPU())
		}
		if rc.PayloadLogMaxBytes <= 0 {
			rc.PayloadLogMaxBytes = c.Logging.PayloadLogMaxBytes
		}
		if rc.SocketRecvBuffer <= 0 {
			rc.SocketRecvBuffer = DefaultReceiverSocketRecvBuffer
		}
		if rc.Type == "kafka" {
			if rc.DialTimeout == "" {
				rc.DialTimeout = DefaultKafkaDialTimeout
			}
			if rc.ConnIdleTimeout == "" {
				rc.ConnIdleTimeout = DefaultKafkaConnIdleTimeout
			}
			if rc.MetadataMaxAge == "" {
				rc.MetadataMaxAge = DefaultKafkaMetadataMaxAge
			}
			if rc.RetryBackoff == "" {
				rc.RetryBackoff = DefaultKafkaRetryBackoff
			}
			if rc.SessionTimeout == "" {
				rc.SessionTimeout = DefaultKafkaReceiverSessionTTL
			}
			if rc.HeartbeatInterval == "" {
				rc.HeartbeatInterval = DefaultKafkaReceiverHeartbeat
			}
			if rc.RebalanceTimeout == "" {
				rc.RebalanceTimeout = DefaultKafkaReceiverRebalanceTTL
			}
			if rc.Balancers == nil {
				rc.Balancers = append([]string(nil), DefaultKafkaReceiverBalancers...)
			}
			if rc.AutoCommit == nil {
				v := DefaultKafkaReceiverAutoCommit
				rc.AutoCommit = &v
			}
			if BoolValue(rc.AutoCommit) && rc.AutoCommitInterval == "" {
				rc.AutoCommitInterval = DefaultKafkaReceiverAutoCommitIv
			}
			if rc.FetchMaxPartitionBytes <= 0 {
				rc.FetchMaxPartitionBytes = DefaultKafkaFetchMaxPartBytes
			}
			if rc.IsolationLevel == "" {
				rc.IsolationLevel = DefaultKafkaIsolationLevel
			}
		}
		if rc.Type == "dds_skydds" {
			if rc.WaitTimeout == "" {
				rc.WaitTimeout = DefaultSkyDDSWaitTimeout
			}
			if rc.DrainMaxItems <= 0 {
				rc.DrainMaxItems = DefaultSkyDDSDrainMaxItems
			}
		}
		c.Receivers[name] = rc
	}
	for name, sc := range c.Senders {
		if sc.Concurrency <= 0 {
			sc.Concurrency = DefaultSenderConcurrency
		}
		if sc.SocketSendBuffer <= 0 {
			sc.SocketSendBuffer = DefaultSenderSocketSendBuffer
		}
		if sc.Type == "kafka" {
			if sc.DialTimeout == "" {
				sc.DialTimeout = DefaultKafkaDialTimeout
			}
			if sc.RequestTimeout == "" {
				sc.RequestTimeout = DefaultKafkaSenderRequestTimeout
			}
			if sc.RetryTimeout == "" {
				sc.RetryTimeout = DefaultKafkaRetryTimeout
			}
			if sc.RetryBackoff == "" {
				sc.RetryBackoff = DefaultKafkaRetryBackoff
			}
			if sc.ConnIdleTimeout == "" {
				sc.ConnIdleTimeout = DefaultKafkaConnIdleTimeout
			}
			if sc.MetadataMaxAge == "" {
				sc.MetadataMaxAge = DefaultKafkaMetadataMaxAge
			}
			if sc.Partitioner == "" {
				sc.Partitioner = DefaultKafkaSenderPartitioner
			}
		}
		c.Senders[name] = sc
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
