package config

import "runtime"

const (
	defaultTaskPoolPerCPUFactor  = 256
	defaultTaskQueuePerCPUFactor = 512
)

// Merge 将系统配置与业务配置拼装为运行时使用的完整 Config。
func (s SystemConfig) Merge(b BusinessConfig) Config {
	cfg := Config{
		Version:   b.Version,
		Control:   s.Control,
		Logging:   s.Logging,
		Receivers: b.Receivers,
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
		if tc.QueueSize <= 0 && d.Task.QueueSize > 0 {
			tc.QueueSize = d.Task.QueueSize
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
		if d.Receiver.Multicore != nil {
			rc.Multicore = *d.Receiver.Multicore
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
	if c.Control.PprofPort < 0 {
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
	if c.Logging.TrafficStatsInterval == "" {
		c.Logging.TrafficStatsInterval = DefaultTrafficStatsInterval
	}
	if c.Logging.TrafficStatsSampleEvery <= 0 {
		c.Logging.TrafficStatsSampleEvery = DefaultTrafficStatsSampleEvery
	}
	if c.Logging.PayloadLogMaxBytes <= 0 {
		c.Logging.PayloadLogMaxBytes = DefaultPayloadLogMaxBytes
	}
	if c.Logging.PayloadPoolMaxCachedBytes < 0 {
		c.Logging.PayloadPoolMaxCachedBytes = DefaultPayloadPoolMaxCachedBytes
	}
	for name, tc := range c.Tasks {
		profile := c.inferTaskProtocol(tc)
		if tc.ExecutionModel == "" {
			tc.ExecutionModel = defaultTaskExecutionModel(profile)
		}
		if tc.PoolSize <= 0 {
			tc.PoolSize = defaultTaskPoolSize(profile)
		}
		if tc.QueueSize <= 0 {
			tc.QueueSize = defaultTaskQueueSize(profile)
		}
		if tc.ChannelQueueSize <= 0 {
			tc.ChannelQueueSize = tc.QueueSize
		}
		if tc.PayloadLogMaxBytes <= 0 {
			tc.PayloadLogMaxBytes = c.Logging.PayloadLogMaxBytes
		}
		c.Tasks[name] = tc
	}
	for name, rc := range c.Receivers {
		if !rc.Multicore {
			rc.Multicore = defaultReceiverMulticore(rc.Type)
		}
		if rc.NumEventLoop <= 0 {
			rc.NumEventLoop = defaultReceiverEventLoops(rc.Type)
		}
		if rc.PayloadLogMaxBytes <= 0 {
			rc.PayloadLogMaxBytes = c.Logging.PayloadLogMaxBytes
		}
		c.Receivers[name] = rc
	}
	for name, sc := range c.Senders {
		if sc.Concurrency <= 0 {
			sc.Concurrency = defaultSenderConcurrency(sc.Type)
		}
		c.Senders[name] = sc
	}
}

func (c *Config) inferTaskProtocol(tc TaskConfig) string {
	for _, rn := range tc.Receivers {
		rc, ok := c.Receivers[rn]
		if !ok {
			continue
		}
		if p := normalizeProtocol(rc.Type); p != "" {
			return p
		}
	}
	for _, sn := range tc.Senders {
		sc, ok := c.Senders[sn]
		if !ok {
			continue
		}
		if p := normalizeProtocol(sc.Type); p != "" {
			return p
		}
	}
	return ""
}

func normalizeProtocol(tp string) string {
	switch tp {
	case "udp_gnet", "udp_unicast", "udp_multicast":
		return "udp"
	case "tcp_gnet":
		return "tcp"
	case "kafka", "sftp":
		return tp
	default:
		return ""
	}
}

func defaultTaskExecutionModel(proto string) string {
	if proto == "udp" || proto == "tcp" {
		return "fastpath"
	}
	return "pool"
}

func defaultTaskPoolSize(proto string) int {
	if proto == "udp" || proto == "tcp" {
		return max(512, runtime.NumCPU()*defaultTaskPoolPerCPUFactor)
	}
	return max(DefaultTaskPoolSize, runtime.NumCPU()*defaultTaskPoolPerCPUFactor)
}

func defaultTaskQueueSize(proto string) int {
	if proto == "udp" || proto == "tcp" {
		return max(1024, runtime.NumCPU()*defaultTaskQueuePerCPUFactor)
	}
	return max(DefaultTaskQueueSize, runtime.NumCPU()*defaultTaskQueuePerCPUFactor)
}

func defaultReceiverMulticore(receiverType string) bool {
	if normalizeProtocol(receiverType) == "udp" {
		return false
	}
	return DefaultReceiverMulticore
}

func defaultReceiverEventLoops(receiverType string) int {
	if normalizeProtocol(receiverType) == "udp" {
		return clamp(runtime.NumCPU()/2, 1, 8)
	}
	return max(DefaultReceiverNumEventLoop, runtime.NumCPU())
}

func defaultSenderConcurrency(senderType string) int {
	switch normalizeProtocol(senderType) {
	case "udp":
		return clamp(runtime.NumCPU()/2, 2, 8)
	case "tcp":
		return clamp(runtime.NumCPU(), 2, 16)
	case "kafka":
		return clamp(runtime.NumCPU()/2, 2, 8)
	case "sftp":
		return clamp(runtime.NumCPU()/2, 1, 4)
	default:
		return DefaultSenderConcurrency
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
