// config.go 定义系统配置结构体以及各模块配置字段。
package config

const (
	DefaultControlTimeoutSec       = 5
	DefaultLogLevel                = "info"
	DefaultTrafficStatsInterval    = "1s"
	DefaultTrafficStatsSampleEvery = 1
	DefaultLogRotateMaxSizeMB      = 100
	DefaultLogRotateMaxBackups     = 5
	DefaultLogRotateMaxAgeDays     = 30
	DefaultLogRotateCompress       = true
)

type Config struct {
	Version   int64                     `json:"version"`
	Control   ControlConfig             `json:"control,omitempty"`
	Logging   LoggingConfig             `json:"logging"`
	Receivers map[string]ReceiverConfig `json:"receivers"`
	Senders   map[string]SenderConfig   `json:"senders"`
	Pipelines map[string][]StageConfig  `json:"pipelines"`
	Tasks     map[string]TaskConfig     `json:"tasks"`
}

type ControlConfig struct {
	API        string `json:"api,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

type LoggingConfig struct {
	Level                   string `json:"level"`
	File                    string `json:"file"`
	MaxSizeMB               int    `json:"max_size_mb,omitempty"`
	MaxBackups              int    `json:"max_backups,omitempty"`
	MaxAgeDays              int    `json:"max_age_days,omitempty"`
	Compress                *bool  `json:"compress,omitempty"`
	TrafficStatsInterval    string `json:"traffic_stats_interval,omitempty"`
	TrafficStatsSampleEvery int    `json:"traffic_stats_sample_every,omitempty"`
}

type ReceiverConfig struct {
	Type      string `json:"type"` // udp_gnet | tcp_gnet | kafka | sftp
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

	RemoteDir       string `json:"remote_dir,omitempty"`
	PollIntervalSec int    `json:"poll_interval_sec,omitempty"`
	ChunkSize       int    `json:"chunk_size,omitempty"`
}

type SenderConfig struct {
	Type        string `json:"type"` // udp_unicast | udp_multicast | tcp_gnet | kafka | sftp
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

	LocalIP   string `json:"local_ip"`
	LocalPort int    `json:"local_port"`
	Iface     string `json:"iface"`
	TTL       int    `json:"ttl"`
	Loop      bool   `json:"loop"`

	RemoteDir  string `json:"remote_dir,omitempty"`
	TempSuffix string `json:"temp_suffix,omitempty"`
}

type StageConfig struct {
	Type   string `json:"type"`
	Offset int    `json:"offset,omitempty"`
	Hex    string `json:"hex,omitempty"`
	Flag   string `json:"flag,omitempty"`
	Path   string `json:"path,omitempty"`
	Bool   *bool  `json:"bool,omitempty"`
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
}
