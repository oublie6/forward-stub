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

type ReceiverConfig struct {
	Type      string `json:"type"` // udp_gnet | tcp_gnet | kafka
	Listen    string `json:"listen"`
	Multicore bool   `json:"multicore"`
	Frame     string `json:"frame"` // "" | "u16be" (TCP)
	Topic     string `json:"topic,omitempty"`
	GroupID   string `json:"group_id,omitempty"`
}

type SenderConfig struct {
	Type        string `json:"type"` // udp_unicast | udp_multicast | tcp_gnet | kafka
	Remote      string `json:"remote"`
	Frame       string `json:"frame"`
	Concurrency int    `json:"concurrency"`
	Topic       string `json:"topic"`

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
}
