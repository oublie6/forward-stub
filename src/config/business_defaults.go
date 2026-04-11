package config

import "runtime"

// Merge 将系统配置与业务配置拼装为运行时使用的完整 Config。
func (s SystemConfig) Merge(b BusinessConfig) Config {
	return Config{
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
}

// ApplyDefaults 是配置默认值规范化的总入口。
//
// system 字段只按“system 显式值 > 代码默认值”回写；
// business 字段按“business 显式值 > system.business_defaults > 代码默认值”回写。
func (c *Config) ApplyDefaults(d BusinessDefaultsConfig) {
	if c == nil {
		return
	}
	applySystemDefaults(&c.Control, &c.Logging)
	applyBusinessDefaults(c, d)
}

// ApplyDefaults 只规范化 system 自身字段，用于控制面拉取 business 前确定请求参数。
func (s *SystemConfig) ApplyDefaults() {
	if s == nil {
		return
	}
	applySystemDefaults(&s.Control, &s.Logging)
}

func applySystemDefaults(control *ControlConfig, logging *LoggingConfig) {
	if control.TimeoutSec <= 0 {
		control.TimeoutSec = DefaultControlTimeoutSec
	}
	if control.ConfigWatchInterval == "" {
		control.ConfigWatchInterval = DefaultConfigWatchInterval
	}
	if control.PprofPort == 0 {
		control.PprofPort = DefaultPprofPort
	}
	if logging.Level == "" {
		logging.Level = DefaultLogLevel
	}
	if logging.MaxSizeMB <= 0 {
		logging.MaxSizeMB = DefaultLogRotateMaxSizeMB
	}
	if logging.MaxBackups <= 0 {
		logging.MaxBackups = DefaultLogRotateMaxBackups
	}
	if logging.MaxAgeDays <= 0 {
		logging.MaxAgeDays = DefaultLogRotateMaxAgeDays
	}
	if logging.Compress == nil {
		v := DefaultLogRotateCompress
		logging.Compress = &v
	}
	if logging.GCStatsLogEnabled == nil {
		v := DefaultGCStatsLogEnabled
		logging.GCStatsLogEnabled = &v
	}
	if logging.GCStatsLogInterval == "" {
		logging.GCStatsLogInterval = DefaultGCStatsLogInterval
	}
	if logging.TrafficStatsInterval == "" {
		logging.TrafficStatsInterval = DefaultTrafficStatsInterval
	}
	if logging.TrafficStatsSampleEvery <= 0 {
		logging.TrafficStatsSampleEvery = DefaultTrafficStatsSampleEvery
	}
	if logging.PayloadLogMaxBytes <= 0 {
		logging.PayloadLogMaxBytes = DefaultPayloadLogMaxBytes
	}
}

func applyBusinessDefaults(c *Config, d BusinessDefaultsConfig) {
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
		if rc.SocketRecvBuffer <= 0 && d.Receiver.SocketRecvBuffer > 0 {
			rc.SocketRecvBuffer = d.Receiver.SocketRecvBuffer
		}
		if rc.Type == "kafka" {
			if rc.DialTimeout == "" && d.Receiver.DialTimeout != "" {
				rc.DialTimeout = d.Receiver.DialTimeout
			}
			if rc.ConnIdleTimeout == "" && d.Receiver.ConnIdleTimeout != "" {
				rc.ConnIdleTimeout = d.Receiver.ConnIdleTimeout
			}
			if rc.MetadataMaxAge == "" && d.Receiver.MetadataMaxAge != "" {
				rc.MetadataMaxAge = d.Receiver.MetadataMaxAge
			}
			if rc.RetryBackoff == "" && d.Receiver.RetryBackoff != "" {
				rc.RetryBackoff = d.Receiver.RetryBackoff
			}
			if rc.SessionTimeout == "" && d.Receiver.SessionTimeout != "" {
				rc.SessionTimeout = d.Receiver.SessionTimeout
			}
			if rc.HeartbeatInterval == "" && d.Receiver.HeartbeatInterval != "" {
				rc.HeartbeatInterval = d.Receiver.HeartbeatInterval
			}
			if rc.RebalanceTimeout == "" && d.Receiver.RebalanceTimeout != "" {
				rc.RebalanceTimeout = d.Receiver.RebalanceTimeout
			}
			if rc.Balancers == nil && d.Receiver.Balancers != nil {
				rc.Balancers = append([]string(nil), d.Receiver.Balancers...)
			}
			if rc.AutoCommit == nil && d.Receiver.AutoCommit != nil {
				v := *d.Receiver.AutoCommit
				rc.AutoCommit = &v
			}
			if rc.AutoCommitInterval == "" && d.Receiver.AutoCommitInterval != "" && (rc.AutoCommit == nil || BoolValue(rc.AutoCommit)) {
				rc.AutoCommitInterval = d.Receiver.AutoCommitInterval
			}
			if rc.FetchMaxPartitionBytes <= 0 && d.Receiver.FetchMaxPartitionBytes > 0 {
				rc.FetchMaxPartitionBytes = d.Receiver.FetchMaxPartitionBytes
			}
			if rc.IsolationLevel == "" && d.Receiver.IsolationLevel != "" {
				rc.IsolationLevel = d.Receiver.IsolationLevel
			}
		}
		if rc.Type == "dds_skydds" {
			if rc.WaitTimeout == "" && d.Receiver.WaitTimeout != "" {
				rc.WaitTimeout = d.Receiver.WaitTimeout
			}
			if rc.DrainMaxItems <= 0 && d.Receiver.DrainMaxItems > 0 {
				rc.DrainMaxItems = d.Receiver.DrainMaxItems
			}
			if rc.DrainBufferBytes <= 0 && d.Receiver.DrainBufferBytes > 0 {
				rc.DrainBufferBytes = d.Receiver.DrainBufferBytes
			}
		}
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
			if rc.DrainBufferBytes <= 0 {
				rc.DrainBufferBytes = DefaultSkyDDSDrainBufferBytes
			}
		}
		c.Receivers[name] = rc
	}
	for name, sc := range c.Senders {
		if sc.Concurrency <= 0 && d.Sender.Concurrency > 0 {
			sc.Concurrency = d.Sender.Concurrency
		}
		if sc.SocketSendBuffer <= 0 && d.Sender.SocketSendBuffer > 0 {
			sc.SocketSendBuffer = d.Sender.SocketSendBuffer
		}
		if sc.Type == "kafka" {
			if sc.DialTimeout == "" && d.Sender.DialTimeout != "" {
				sc.DialTimeout = d.Sender.DialTimeout
			}
			if sc.RequestTimeout == "" && d.Sender.RequestTimeout != "" {
				sc.RequestTimeout = d.Sender.RequestTimeout
			}
			if sc.RetryTimeout == "" && d.Sender.RetryTimeout != "" {
				sc.RetryTimeout = d.Sender.RetryTimeout
			}
			if sc.RetryBackoff == "" && d.Sender.RetryBackoff != "" {
				sc.RetryBackoff = d.Sender.RetryBackoff
			}
			if sc.ConnIdleTimeout == "" && d.Sender.ConnIdleTimeout != "" {
				sc.ConnIdleTimeout = d.Sender.ConnIdleTimeout
			}
			if sc.MetadataMaxAge == "" && d.Sender.MetadataMaxAge != "" {
				sc.MetadataMaxAge = d.Sender.MetadataMaxAge
			}
			if sc.Partitioner == "" && d.Sender.Partitioner != "" {
				sc.Partitioner = d.Sender.Partitioner
			}
		}
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
