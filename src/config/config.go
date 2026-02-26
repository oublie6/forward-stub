// config.go 定义系统配置结构体以及各模块配置字段。
package config

const (
	DefaultControlTimeoutSec        = 5
	DefaultLogLevel                 = "info"
	DefaultTrafficStatsInterval     = "1s"
	DefaultTrafficStatsSampleEvery  = 1
	DefaultTrafficStatsEnableSender = true
	DefaultLogRotateMaxSizeMB       = 100
	DefaultLogRotateMaxBackups      = 5
	DefaultLogRotateMaxAgeDays      = 30
	DefaultLogRotateCompress        = true
	DefaultPprofListen              = "127.0.0.1:6060"
	DefaultKafkaSendTimeoutMS       = 5000
	DefaultTaskPoolSize             = 64
	DefaultKafkaTLS                 = false
	DefaultPayloadPoolSize          = 1024
	DefaultPayloadMaxReuseBytes     = 0
)

type Config struct {
	Version   int64                     `json:"version"`
	Control   ControlConfig             `json:"control,omitempty"`
	Logging   LoggingConfig             `json:"logging"`
	Pprof     PprofConfig               `json:"pprof,omitempty"`
	Runtime   RuntimeConfig             `json:"runtime,omitempty"`
	Receivers map[string]ReceiverConfig `json:"receivers"`
	Senders   map[string]SenderConfig   `json:"senders"`
	Pipelines map[string][]StageConfig  `json:"pipelines"`
	Tasks     map[string]TaskConfig     `json:"tasks"`
}

type RuntimeConfig struct {
	DefaultTaskPoolSize  int `json:"default_task_pool_size,omitempty"`
	PayloadPoolSize      int `json:"payload_pool_size,omitempty"`
	PayloadMaxReuseBytes int `json:"payload_max_reuse_bytes,omitempty"`
}

type ControlConfig struct {
	API        string `json:"api,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

type LoggingConfig struct {
	Level                    string `json:"level"`
	File                     string `json:"file"`
	MaxSizeMB                int    `json:"max_size_mb,omitempty"`
	MaxBackups               int    `json:"max_backups,omitempty"`
	MaxAgeDays               int    `json:"max_age_days,omitempty"`
	Compress                 *bool  `json:"compress,omitempty"`
	TrafficStatsInterval     string `json:"traffic_stats_interval,omitempty"`
	TrafficStatsSampleEvery  int    `json:"traffic_stats_sample_every,omitempty"`
	TrafficStatsEnableSender *bool  `json:"traffic_stats_enable_sender,omitempty"`
}

type PprofConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Listen  string `json:"listen,omitempty"`
}

type ReceiverConfig struct {
	Type      string `json:"type"` // udp_gnet | tcp_gnet | kafka
	Listen    string `json:"listen"`
	Multicore bool   `json:"multicore"`
	Frame     string `json:"frame"` // "" | "u16be" (TCP)
	Topic     string `json:"topic,omitempty"`
	GroupID   string `json:"group_id,omitempty"`

	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	SASLMechanism string `json:"sasl_mechanism,omitempty"` // 当前支持 PLAIN
	TLS           bool   `json:"tls,omitempty"`
	TLSSkipVerify bool   `json:"tls_skip_verify,omitempty"`
	ClientID      string `json:"client_id,omitempty"`

	StartOffset    string `json:"start_offset,omitempty"` // earliest | latest
	FetchMinBytes  int    `json:"fetch_min_bytes,omitempty"`
	FetchMaxBytes  int    `json:"fetch_max_bytes,omitempty"`
	FetchMaxWaitMS int    `json:"fetch_max_wait_ms,omitempty"`
}

type SenderConfig struct {
	Type        string `json:"type"` // udp_unicast | udp_multicast | tcp_gnet | kafka
	Remote      string `json:"remote"`
	Frame       string `json:"frame"`
	Concurrency int    `json:"concurrency"`
	Topic       string `json:"topic"`

	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	SASLMechanism string `json:"sasl_mechanism,omitempty"` // 当前支持 PLAIN
	TLS           bool   `json:"tls,omitempty"`
	TLSSkipVerify bool   `json:"tls_skip_verify,omitempty"`
	ClientID      string `json:"client_id,omitempty"`

	Acks          int    `json:"acks,omitempty"`            // -1(all) / 1 / 0
	LingerMS      int    `json:"linger_ms,omitempty"`       // 默认 1ms
	BatchMaxBytes int    `json:"batch_max_bytes,omitempty"` // 默认 1MiB
	Compression   string `json:"compression,omitempty"`     // none|gzip|snappy|lz4|zstd
	SendTimeoutMS int    `json:"send_timeout_ms,omitempty"` // Kafka ProduceSync 超时，默认 5000ms

	LocalIP   string `json:"local_ip"`
	LocalPort int    `json:"local_port"`
	Iface     string `json:"iface"`
	TTL       int    `json:"ttl"`
	Loop      bool   `json:"loop"`
}

type StageConfig struct {
	Type   string `json:"type"`
	Offset int    `json:"offset,omitempty"`
	Hex    string `json:"hex,omitempty"`
	Flag   string `json:"flag,omitempty"`
}

type TaskConfig struct {
	PoolSize  int      `json:"pool_size"`
	FastPath  bool     `json:"fast_path"`
	Receivers []string `json:"receivers"`
	Pipelines []string `json:"pipelines"`
	Senders   []string `json:"senders"`
}

// ApplyDefaults 为 receiver/task/sender 之外的配置字段填充默认值。
func (c *Config) ApplyDefaults() {
	if c == nil {
		return
	}
	if c.Control.TimeoutSec <= 0 {
		c.Control.TimeoutSec = DefaultControlTimeoutSec
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
	if c.Logging.TrafficStatsEnableSender == nil {
		v := DefaultTrafficStatsEnableSender
		c.Logging.TrafficStatsEnableSender = &v
	}
	if c.Pprof.Listen == "" {
		c.Pprof.Listen = DefaultPprofListen
	}
	if c.Runtime.DefaultTaskPoolSize <= 0 {
		c.Runtime.DefaultTaskPoolSize = DefaultTaskPoolSize
	}
	if c.Runtime.PayloadPoolSize <= 0 {
		c.Runtime.PayloadPoolSize = DefaultPayloadPoolSize
	}
	if c.Runtime.PayloadMaxReuseBytes < 0 {
		c.Runtime.PayloadMaxReuseBytes = DefaultPayloadMaxReuseBytes
	}
}
