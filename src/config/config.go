// config.go 定义系统配置结构体以及各模块配置字段。
package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	DefaultControlTimeoutSec         = 5
	DefaultConfigWatchInterval       = "2s"
	DefaultPprofPort                 = 6060
	DefaultLogLevel                  = "info"
	DefaultTrafficStatsInterval      = "1s"
	DefaultTrafficStatsSampleEvery   = 1
	DefaultLogRotateMaxSizeMB        = 100
	DefaultLogRotateMaxBackups       = 5
	DefaultLogRotateMaxAgeDays       = 30
	DefaultLogRotateCompress         = true
	DefaultGCStatsLogEnabled         = false
	DefaultGCStatsLogInterval        = "1m"
	DefaultPayloadLogMaxBytes        = 256
	DefaultReceiverMulticore         = true
	DefaultReceiverNumEventLoop      = 8
	DefaultReceiverSocketRecvBuffer  = 1 << 30
	DefaultSenderConcurrency         = 8
	DefaultSenderSocketSendBuffer    = 1 << 30
	DefaultTaskPoolSize              = 4096
	DefaultTaskQueueSize             = 8192
	DefaultPayloadPoolMaxCachedBytes = int64(0)
	DefaultKafkaDialTimeout          = "10s"
	DefaultKafkaSenderRequestTimeout = "30s"
	DefaultKafkaRetryTimeout         = "1m"
	DefaultKafkaRetryBackoff         = "250ms"
	DefaultKafkaConnIdleTimeout      = "30s"
	DefaultKafkaMetadataMaxAge       = "5m"
	DefaultKafkaSenderPartitioner    = "sticky"
	DefaultKafkaReceiverSessionTTL   = "45s"
	DefaultKafkaReceiverHeartbeat    = "3s"
	DefaultKafkaReceiverRebalanceTTL = "1m"
	DefaultKafkaReceiverAutoCommit   = true
	DefaultKafkaReceiverAutoCommitIv = "5s"
	DefaultKafkaFetchMaxPartBytes    = 1 << 20
	DefaultKafkaIsolationLevel       = "read_uncommitted"
)

var DefaultKafkaReceiverBalancers = []string{"cooperative_sticky"}

// Config 是系统运行时的全量配置快照。
//
// 使用方式：
//  1. 通过 local_load 反序列化 JSON 到 Config；
//  2. 调用 ApplyDefaults 填充通用默认值；
//  3. 调用 Validate 校验语义；
//  4. 交给 runtime 编译并热切换组件。
type Config struct {
	// Version 是配置版本号（通常由控制面递增）。
	// 用法：runtime 可据此判断新旧配置并决定是否触发热更新。
	Version int64 `json:"version"`
	// Control 是控制面相关配置。
	// 用法：填写 API + TimeoutSec 后可启用远端拉取；留空 API 则仅本地配置生效。
	Control ControlConfig `json:"control,omitempty"`
	// Logging 是日志输出与流量统计配置。
	// 用法：生产环境建议配置 file 与滚动参数，便于留存与排障。
	Logging LoggingConfig `json:"logging"`
	// Receivers 是接收端定义表，key 为实例名。
	// 用法：每个 receiver 负责协议接入，并绑定一个 selector 参与路由。
	Receivers map[string]ReceiverConfig `json:"receivers"`
	// Selectors 是 selector 定义表，key 为 selector 名称。
	// 用法：receiver.Selector 引用这里的 key；selector 只做 match key 精确匹配。
	Selectors map[string]SelectorConfig `json:"selectors"`
	// TaskSets 是 task 集合定义表，key 为 task set 名称。
	// 用法：selector 通过 task_set 名称复用一组 task；运行时会直接展开到 task slice。
	TaskSets map[string][]string `json:"task_sets"`
	// Senders 是发送端定义表，key 为实例名。
	// 用法：task.Senders 引用这里的 key，可让多个 task 复用同一 sender 连接。
	Senders map[string]SenderConfig `json:"senders"`
	// Pipelines 是处理流水线定义表，key 为 pipeline 名称。
	// 用法：task.Pipelines 按顺序引用 pipeline 名称，从而编排处理链路。
	Pipelines map[string][]StageConfig `json:"pipelines"`
	// Tasks 是任务定义表，key 为任务名。
	// 用法：每个 task 定义 pipeline + sender + execution_model，由 selector 决定何时命中。
	Tasks map[string]TaskConfig `json:"tasks"`
}

// SystemConfig 仅包含需要重启后生效的系统级配置。
type SystemConfig struct {
	Control          ControlConfig          `json:"control,omitempty"`
	Logging          LoggingConfig          `json:"logging"`
	BusinessDefaults BusinessDefaultsConfig `json:"business_defaults,omitempty"`
}

// BusinessDefaultsConfig 定义 business.config 中可选字段的系统级默认值。
type BusinessDefaultsConfig struct {
	Task     TaskDefaultConfig     `json:"task,omitempty"`
	Receiver ReceiverDefaultConfig `json:"receiver,omitempty"`
	Sender   SenderDefaultConfig   `json:"sender,omitempty"`
}

// TaskDefaultConfig 定义 task 的系统级默认值。
type TaskDefaultConfig struct {
	PoolSize           int    `json:"pool_size,omitempty"`
	QueueSize          int    `json:"queue_size,omitempty"`
	ChannelQueueSize   int    `json:"channel_queue_size,omitempty"`
	ExecutionModel     string `json:"execution_model,omitempty"`
	PayloadLogMaxBytes int    `json:"payload_log_max_bytes,omitempty"`
}

// ReceiverDefaultConfig 定义 receiver 的系统级默认值。
type ReceiverDefaultConfig struct {
	Multicore          *bool `json:"multicore,omitempty"`
	NumEventLoop       int   `json:"num_event_loop,omitempty"`
	PayloadLogMaxBytes int   `json:"payload_log_max_bytes,omitempty"`
}

// SenderDefaultConfig 定义 sender 的系统级默认值。
type SenderDefaultConfig struct {
	Concurrency int `json:"concurrency,omitempty"`
}

// BusinessConfig 仅包含支持热重载的业务拓扑配置。
type BusinessConfig struct {
	Version   int64                     `json:"version"`
	Receivers map[string]ReceiverConfig `json:"receivers"`
	Selectors map[string]SelectorConfig `json:"selectors"`
	TaskSets  map[string][]string       `json:"task_sets"`
	Senders   map[string]SenderConfig   `json:"senders"`
	Pipelines map[string][]StageConfig  `json:"pipelines"`
	Tasks     map[string]TaskConfig     `json:"tasks"`
}

// ControlConfig 描述远端配置中心参数。
type ControlConfig struct {
	// API 是控制面接口地址（例如 http(s)://host:port/path）。
	// 用法：配置后 runtime 会在启动与重载时通过控制面拉取配置；留空表示禁用远端拉取。
	API string `json:"api,omitempty"`
	// TimeoutSec 是控制面请求超时时间（秒）。
	// 用法：网络较慢场景可适度调大，避免误判拉取失败。
	TimeoutSec int `json:"timeout_sec,omitempty"`
	// ConfigWatchInterval 是本地业务配置文件变更检测间隔，如 "2s"。
	// 用法：值越小热更新越及时，但会增加文件读取开销。
	ConfigWatchInterval string `json:"config_watch_interval,omitempty"`
	// PprofPort 是 pprof HTTP 服务端口；0 表示使用默认值，-1 表示禁用。
	// 用法：配置后可通过 /debug/pprof/* 触发采集与诊断。
	PprofPort int `json:"pprof_port,omitempty"`
}

// LoggingConfig 描述日志输出与流量统计参数。
type LoggingConfig struct {
	// Level 控制日志最小输出级别（debug/info/warn/error）。
	// 用法：排障时可临时升到 debug，生产常用 info/warn。
	Level string `json:"level"`
	// File 为日志文件路径；为空时输出到 stderr。
	// 用法：建议在服务化部署时配置到持久卷，配合轮转参数统一管理。
	File string `json:"file"`
	// MaxSizeMB 是单个日志分卷最大大小（MB）。
	// 用法：达到阈值触发滚动，避免单文件过大影响采集与检索。
	MaxSizeMB int `json:"max_size_mb,omitempty"`
	// MaxBackups 是历史日志分卷保留数量。
	// 用法：与 MaxAgeDays 共同控制磁盘占用上限。
	MaxBackups int `json:"max_backups,omitempty"`
	// MaxAgeDays 是历史日志分卷保留天数。
	// 用法：满足合规审计时可调大，磁盘紧张时可调小。
	MaxAgeDays int `json:"max_age_days,omitempty"`
	// Compress 控制是否压缩历史分卷。
	// 用法：建议生产开启（true），在 CPU 紧张时可权衡关闭。
	Compress *bool `json:"compress,omitempty"`
	// TrafficStatsInterval 是流量统计日志输出周期，如 "5s"。
	// 用法：周期越短可观测性越强，但日志量也越大。
	TrafficStatsInterval string `json:"traffic_stats_interval,omitempty"`
	// TrafficStatsSampleEvery 为采样倍率，1 表示全量统计。
	// 用法：高吞吐场景可调大以降低统计开销。
	TrafficStatsSampleEvery int `json:"traffic_stats_sample_every,omitempty"`
	// PayloadLogMaxBytes 是 payload 摘要日志的默认最大字节数。
	// 用法：receiver/task 未单独配置时使用该值；建议设置为 64~1024。
	PayloadLogMaxBytes int `json:"payload_log_max_bytes,omitempty"`
	// PayloadPoolMaxCachedBytes 控制 payload 内存池可缓存的总字节上限。
	// 用法：<=0 表示不限制（默认）；>0 可限制缓存内存占用峰值。
	PayloadPoolMaxCachedBytes int64 `json:"payload_pool_max_cached_bytes,omitempty"`
	// GCStatsLogEnabled 控制是否开启周期性 GC / 内存 / goroutine 信息日志。
	// 用法：排障或容量评估时开启；常规生产环境可按需关闭。
	GCStatsLogEnabled *bool `json:"gc_stats_log_enabled,omitempty"`
	// GCStatsLogInterval 控制 GC 统计日志周期，如 "1m"。
	// 用法：仅在 gc_stats_log_enabled=true 时生效，必须能被 time.ParseDuration 解析且 >0。
	GCStatsLogInterval string `json:"gc_stats_log_interval,omitempty"`
}

// ReceiverConfig 描述单个接收端实例。
type ReceiverConfig struct {
	// Type 指定接收端实现类型（udp_gnet/tcp_gnet/kafka/sftp）。
	// 用法：不同 type 决定下列字段的生效范围，应按协议填写配套参数。
	Type string `json:"type"` // udp_gnet | tcp_gnet | kafka | sftp
	// Listen 为监听地址；Kafka 场景下为 brokers 列表。
	// 用法：udp/tcp 填 host:port；kafka 填逗号分隔 broker；sftp 填服务器 host:port。
	Listen string `json:"listen"`
	// Multicore 控制 gnet 是否启用多核事件循环。
	// 用法：仅对 gnet 协议有效，在多核机器上可提升吞吐。
	Multicore *bool `json:"multicore,omitempty"`
	// NumEventLoop 显式指定 gnet event-loop 数量（<=0 表示使用 gnet 默认值）。
	// 用法：仅对 gnet 协议有效；在 CPU 绑定压测场景可精细控制并发模型。
	NumEventLoop int `json:"num_event_loop,omitempty"`
	// ReadBufferCap 显式指定 gnet 每连接读缓冲上限（字节，<=0 表示使用 gnet 默认值）。
	// 用法：仅对 gnet 协议有效；大包场景可适度调大以减少扩容与拷贝。
	ReadBufferCap int `json:"read_buffer_cap,omitempty"`
	// SocketRecvBuffer 显式指定 socket 内核接收缓冲区大小（字节，<=0 表示使用系统兜底默认值）。
	// 用法：仅对 gnet 协议有效；高流量场景可调大降低内核丢包概率。
	SocketRecvBuffer int `json:"socket_recv_buffer,omitempty"`
	// Frame 是 TCP 粘包拆包规则（空或 u16be）。
	// 用法：收发两端需一致；u16be 表示以 2 字节大端长度前缀分帧。
	Frame string `json:"frame"` // "" | "u16be" (TCP)
	// Selector 是当前 receiver 绑定的 selector 名称。
	// 用法：receiver 只负责生成 match key，selector 再把 key 映射到 task set。
	Selector string `json:"selector"`
	// Topic 是 Kafka 消费主题名。
	// 用法：Type=kafka 时必填，指向要订阅的数据流。
	Topic string `json:"topic,omitempty"`
	// GroupID 是 Kafka 消费组 ID。
	// 用法：同组实例共享分区负载，不同组可独立消费同一主题。
	GroupID string `json:"group_id,omitempty"`

	// Username 是 Kafka/SFTP 的登录用户名。
	// 用法：按目标系统账号策略配置，建议使用最小权限账号。
	Username string `json:"username,omitempty"`
	// Password 是 Kafka/SFTP 的登录密码。
	// 用法：建议通过密文配置或环境注入，避免明文落库。
	Password string `json:"password,omitempty"`
	// SASLMechanism 是 Kafka 鉴权机制，当前仅支持 PLAIN。
	// 用法：Type=kafka 且启用账号鉴权时填写 PLAIN。
	SASLMechanism string `json:"sasl_mechanism,omitempty"` // 当前支持 PLAIN
	// TLS 控制 Kafka 连接是否启用 TLS。
	// 用法：生产建议开启，确保链路加密。
	TLS bool `json:"tls,omitempty"`
	// TLSSkipVerify 控制是否跳过 TLS 证书校验（仅测试环境建议使用）。
	// 用法：仅在自签证书调试阶段使用，生产应保持 false。
	TLSSkipVerify bool `json:"tls_skip_verify,omitempty"`
	// ClientID 是 Kafka 客户端标识。
	// 用法：建议设置为实例名，便于 Kafka 端审计与监控定位。
	ClientID string `json:"client_id,omitempty"`
	// DialTimeout 是 Kafka 建连超时，如 "10s"。
	// 用法：映射 franz-go / kgo 的 DialTimeout；仅 Type=kafka 时生效，必须是合法正数时长。
	DialTimeout string `json:"dial_timeout,omitempty"`
	// ConnIdleTimeout 是 Kafka 空闲连接回收超时，如 "30s"。
	// 用法：映射 kgo.ConnIdleTimeout；仅 Type=kafka 时生效，调大可减少重连，调小可更快回收空闲连接。
	ConnIdleTimeout string `json:"conn_idle_timeout,omitempty"`
	// MetadataMaxAge 是 Kafka 元数据缓存最大存活时间，如 "5m"。
	// 用法：映射 kgo.MetadataMaxAge；仅 Type=kafka 时生效，调小可更快感知分区变化，代价是更多 metadata 请求。
	MetadataMaxAge string `json:"metadata_max_age,omitempty"`
	// RetryBackoff 是 Kafka 可重试请求的退避间隔，如 "250ms"。
	// 用法：映射 kgo.RetryBackoffFn；仅 Type=kafka 时生效，用于控制重试节奏。
	RetryBackoff string `json:"retry_backoff,omitempty"`

	// StartOffset 指定 Kafka 启动消费位点（earliest/latest）。
	// 用法：首次启动追历史数据用 earliest，只消费新数据用 latest。
	StartOffset string `json:"start_offset,omitempty"` // earliest | latest
	// SessionTimeout 是 Kafka consumer group 会话超时，如 "45s"。
	// 用法：映射 kgo.SessionTimeout；仅 Type=kafka 时生效，应大于 heartbeat_interval。
	SessionTimeout string `json:"session_timeout,omitempty"`
	// HeartbeatInterval 是 Kafka consumer group 心跳间隔，如 "3s"。
	// 用法：映射 kgo.HeartbeatInterval；仅 Type=kafka 时生效，通常应显著小于 session_timeout。
	HeartbeatInterval string `json:"heartbeat_interval,omitempty"`
	// RebalanceTimeout 是 Kafka consumer group 重平衡超时，如 "1m"。
	// 用法：映射 kgo.RebalanceTimeout；仅 Type=kafka 时生效，控制成员在 rebalance 中允许占用的最长时间。
	RebalanceTimeout string `json:"rebalance_timeout,omitempty"`
	// Balancers 指定 Kafka consumer group 分配策略列表。
	// 用法：映射 kgo.Balancers；仅 Type=kafka 时生效，当前支持 range / round_robin / cooperative_sticky。
	Balancers []string `json:"balancers,omitempty"`
	// AutoCommit 控制 Kafka receiver 是否启用自动提交位点。
	// 用法：映射 kgo.DisableAutoCommit / AutoCommitInterval；仅 Type=kafka 时生效，nil 表示使用默认 true。
	AutoCommit *bool `json:"auto_commit,omitempty"`
	// AutoCommitInterval 是 Kafka 自动提交位点的周期，如 "5s"。
	// 用法：映射 kgo.AutoCommitInterval；仅 Type=kafka 且 auto_commit=true 时生效。
	AutoCommitInterval string `json:"auto_commit_interval,omitempty"`
	// FetchMinBytes 是 Kafka 拉取最小字节数。
	// 用法：调大可提高批量效率，但会增加端到端时延。
	FetchMinBytes int `json:"fetch_min_bytes,omitempty"`
	// FetchMaxBytes 是 Kafka 单次拉取最大字节数。
	// 用法：需大于典型消息尺寸，避免频繁截断批次。
	FetchMaxBytes int `json:"fetch_max_bytes,omitempty"`
	// FetchMaxPartitionBytes 是 Kafka 单分区单次拉取最大字节数。
	// 用法：映射 kgo.FetchMaxPartitionBytes；仅 Type=kafka 时生效，应不大于 fetch_max_bytes。
	FetchMaxPartitionBytes int `json:"fetch_max_partition_bytes,omitempty"`
	// IsolationLevel 控制 Kafka 拉取隔离级别。
	// 用法：映射 kgo.FetchIsolationLevel；仅 Type=kafka 时生效，当前支持 read_uncommitted / read_committed。
	IsolationLevel string `json:"isolation_level,omitempty"`
	// FetchMaxWaitMS 是 Kafka 拉取最大等待时间（毫秒）。
	// 用法：与 FetchMinBytes 配合调节吞吐与时延平衡。
	FetchMaxWaitMS int `json:"fetch_max_wait_ms,omitempty"`

	// RemoteDir 是 SFTP receiver 轮询读取的远端目录。
	// 用法：应配置为稳定输入目录，并避免和 sender 输出目录冲突。
	RemoteDir string `json:"remote_dir,omitempty"`
	// PollIntervalSec 是 SFTP 目录轮询间隔（秒）。
	// 用法：值越小发现越及时，但对远端目录扫描压力更大。
	PollIntervalSec int `json:"poll_interval_sec,omitempty"`
	// ChunkSize 是 SFTP 文件分块读取大小（字节）。
	// 用法：大块提升吞吐，小块降低单包延迟与内存峰值。
	ChunkSize int `json:"chunk_size,omitempty"`
	// HostKeyFingerprint 是 SFTP 服务端主机公钥指纹（SSH SHA256 格式）。
	// 用法：必须与服务端实际指纹一致，防止中间人攻击；示例：SHA256:AbCd....
	HostKeyFingerprint string `json:"host_key_fingerprint,omitempty"`
	// LogPayloadRecv 控制该 receiver 是否打印接收 payload 日志。
	// 用法：用于定位输入侧问题；建议仅在排障窗口开启。
	LogPayloadRecv bool `json:"log_payload_recv,omitempty"`
	// PayloadLogMaxBytes 控制该 receiver 的 payload 摘要最大字节数。
	// 用法：<=0 时回退到 logging.payload_log_max_bytes。
	PayloadLogMaxBytes int `json:"payload_log_max_bytes,omitempty"`
}

// SenderConfig 描述单个发送端实例。
type SenderConfig struct {
	// Type 指定发送端实现类型（udp_unicast/udp_multicast/tcp_gnet/kafka/sftp）。
	// 用法：根据目标协议选择，字段校验与行为由 type 决定。
	Type string `json:"type"` // udp_unicast | udp_multicast | tcp_gnet | kafka | sftp
	// Remote 是目标地址；Kafka 场景下为 brokers 列表。
	// 用法：udp/tcp/sftp 填 host:port；kafka 填逗号分隔 broker。
	Remote string `json:"remote"`
	// Frame 是 TCP 发送时的封帧规则。
	// 用法：与对端解帧策略匹配，避免粘包拆包错误。
	Frame string `json:"frame"`
	// Concurrency 控制发送并发度（按 sender 类型解释）。
	// 用法：吞吐不足时可增大，但要关注下游承载能力。
	Concurrency int `json:"concurrency"`
	// SocketSendBuffer 显式指定 socket 内核发送缓冲区大小（字节，<=0 表示使用系统兜底默认值）。
	// 用法：对 udp/tcp sender 生效；高吞吐场景建议调大以降低发送侧阻塞。
	SocketSendBuffer int `json:"socket_send_buffer,omitempty"`
	// Topic 是 Kafka 目标主题名。
	// 用法：Type=kafka 时必填，指定消息投递目的主题。
	Topic string `json:"topic"`

	// Username 是 Kafka/SFTP 的登录用户名。
	// 用法：建议使用专用账号，降低权限边界风险。
	Username string `json:"username,omitempty"`
	// Password 是 Kafka/SFTP 的登录密码。
	// 用法：建议从秘密管理系统注入，避免提交到仓库。
	Password string `json:"password,omitempty"`
	// SASLMechanism 是 Kafka 鉴权机制，当前仅支持 PLAIN。
	// 用法：和 Username/Password 配套使用。
	SASLMechanism string `json:"sasl_mechanism,omitempty"` // 当前支持 PLAIN
	// TLS 控制 Kafka 连接是否启用 TLS。
	// 用法：公网或跨网络传输时建议开启。
	TLS bool `json:"tls,omitempty"`
	// TLSSkipVerify 控制是否跳过 TLS 证书校验（仅测试环境建议使用）。
	// 用法：若开启，请同时限制网络暴露范围。
	TLSSkipVerify bool `json:"tls_skip_verify,omitempty"`
	// ClientID 是 Kafka 客户端标识。
	// 用法：用于 broker 端日志追踪与监控分组。
	ClientID string `json:"client_id,omitempty"`
	// DialTimeout 是 Kafka 建连超时，如 "10s"。
	// 用法：映射 franz-go / kgo 的 DialTimeout；仅 Type=kafka 时生效。
	DialTimeout string `json:"dial_timeout,omitempty"`
	// RequestTimeout 是 Kafka Produce 请求超时，如 "30s"。
	// 用法：映射 kgo.ProduceRequestTimeout；仅 Type=kafka 时生效，只约束 Produce 请求在 broker 侧的等待时间。
	RequestTimeout string `json:"request_timeout,omitempty"`
	// RetryTimeout 是 Kafka 请求允许持续重试的总时长，如 "1m"。
	// 用法：映射 kgo.RetryTimeout；仅 Type=kafka 时生效，超时后可重试请求会尽快失败返回。
	RetryTimeout string `json:"retry_timeout,omitempty"`
	// RetryBackoff 是 Kafka 可重试请求的退避间隔，如 "250ms"。
	// 用法：映射 kgo.RetryBackoffFn；仅 Type=kafka 时生效，用于控制重试节奏。
	RetryBackoff string `json:"retry_backoff,omitempty"`
	// ConnIdleTimeout 是 Kafka 空闲连接回收超时，如 "30s"。
	// 用法：映射 kgo.ConnIdleTimeout；仅 Type=kafka 时生效。
	ConnIdleTimeout string `json:"conn_idle_timeout,omitempty"`
	// MetadataMaxAge 是 Kafka 元数据缓存最大存活时间，如 "5m"。
	// 用法：映射 kgo.MetadataMaxAge；仅 Type=kafka 时生效，调小可更快感知 topic / partition 变化。
	MetadataMaxAge string `json:"metadata_max_age,omitempty"`

	// Acks 是 Kafka 生产确认策略（0/1/all 或 -1）。
	// 用法：可靠性优先选 all(-1)，低延迟可考虑 1 或 0。
	Acks KafkaAcksConfig `json:"acks,omitempty"`
	// Idempotent 控制 Kafka sender 是否启用幂等写入。
	// 用法：为空时默认 true；false 时可搭配 acks=0/1 追求低延迟。
	Idempotent *bool `json:"idempotent,omitempty"`
	// Retries 控制 Kafka record 级别重试次数。
	// 用法：<=0 时使用 franz-go 默认值（近似无限重试）。
	Retries int `json:"retries,omitempty"`
	// MaxInFlightRequestsPerConnection 控制单 broker 并发 in-flight produce 请求数。
	// 用法：仅在 idempotent=false 时建议调大；idempotent=true 时必须为 1（或不设置）。
	MaxInFlightRequestsPerConnection int `json:"max_in_flight_requests_per_connection,omitempty"`
	// LingerMS 是 Kafka 批发送聚合等待毫秒数。
	// 用法：调大可提高压缩和吞吐，代价是延迟上升。
	LingerMS int `json:"linger_ms,omitempty"`
	// BatchMaxBytes 是 Kafka 单批次最大字节数。
	// 用法：应与 broker 的 message.max.bytes 限制协同设置。
	BatchMaxBytes int `json:"batch_max_bytes,omitempty"`
	// MaxBufferedBytes 是 Kafka producer 侧可缓冲总字节上限。
	// 用法：映射 franz-go 的 MaxBufferedBytes；<=0 时沿用 franz-go 默认（不限制）。
	MaxBufferedBytes int `json:"max_buffered_bytes,omitempty"`
	// MaxBufferedRecords 是 Kafka producer 侧可缓冲 record 数量上限。
	// 用法：映射 franz-go 的 MaxBufferedRecords；<=0 时沿用 franz-go 默认（10000）。
	MaxBufferedRecords int `json:"max_buffered_records,omitempty"`
	// Compression 是 Kafka 压缩算法（none/gzip/snappy/lz4/zstd）。
	// 用法：带宽紧张时优先启用压缩，CPU 紧张时可用 none。
	Compression string `json:"compression,omitempty"`
	// CompressionLevel 是 Kafka 压缩级别。
	// 用法：映射 franz-go CompressionCodec.WithLevel；仅对 gzip / lz4 / zstd 生效，0 表示使用库默认级别。
	CompressionLevel int `json:"compression_level,omitempty"`
	// Partitioner 指定 Kafka 分区策略。
	// 用法：映射 kgo.RecordPartitioner；当前支持 sticky / round_robin / hash_key。
	Partitioner string `json:"partitioner,omitempty"`
	// RecordKey 是 Kafka record 固定 key。
	// 用法：当所有消息都需要固定 key 时填写；与 record_key_source 互斥。
	RecordKey string `json:"record_key,omitempty"`
	// RecordKeySource 指定 Kafka record key 的来源字段。
	// 用法：当前支持 payload / match_key / remote / local / file_name / file_path / transfer_id / route_sender；与 record_key 互斥。
	RecordKeySource string `json:"record_key_source,omitempty"`

	// LocalIP 是 UDP sender 绑定的本地出接口 IP。
	// 用法：多网卡机器可显式指定出口网络。
	LocalIP string `json:"local_ip"`
	// LocalPort 是 UDP sender 本地源端口。
	// 用法：需要固定源端口给下游做 ACL 时可指定。
	LocalPort int `json:"local_port"`
	// Iface 是组播发送网卡名（udp_multicast）。
	// 用法：在多网卡主机上必须指定正确网卡以确保可达。
	Iface string `json:"iface"`
	// TTL 是组播生存跳数。
	// 用法：局域网一般设置较小值，跨网段按网络规划调高。
	TTL int `json:"ttl"`
	// Loop 控制组播回环。
	// 用法：调试验证时可打开，生产通常关闭避免自收。
	Loop bool `json:"loop"`

	// RemoteDir 是 SFTP sender 最终落盘目录。
	// 用法：确保 sender 账号对该目录有写入与 rename 权限。
	RemoteDir string `json:"remote_dir,omitempty"`
	// TempSuffix 是上传临时文件后缀，完成后 rename 为正式文件。
	// 用法：下游可按后缀过滤未完成文件，避免读到半成品。
	TempSuffix string `json:"temp_suffix,omitempty"`
	// HostKeyFingerprint 是 SFTP 服务端主机公钥指纹（SSH SHA256 格式）。
	// 用法：必须与服务端实际指纹一致，防止中间人攻击；示例：SHA256:AbCd....
	HostKeyFingerprint string `json:"host_key_fingerprint,omitempty"`
}

// KafkaAcksConfig 表示 Kafka sender 的 acks 配置，支持 JSON 数字或字符串。
// 合法值：0、1、-1、"all"。
type KafkaAcksConfig string

// UnmarshalJSON 支持 acks 读取 JSON 数字与字符串。
func (a *KafkaAcksConfig) UnmarshalJSON(data []byte) error {
	if a == nil {
		return fmt.Errorf("nil KafkaAcksConfig")
	}
	if string(data) == "null" {
		*a = ""
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*a = KafkaAcksConfig(strconv.Itoa(n))
		return nil
	}
	var sv string
	if err := json.Unmarshal(data, &sv); err == nil {
		*a = KafkaAcksConfig(strings.TrimSpace(sv))
		return nil
	}
	return fmt.Errorf("invalid kafka acks, must be int or string")
}

// MarshalJSON 统一以字符串输出 acks。
func (a KafkaAcksConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(a))
}

// Int 将 acks 语义映射为 Kafka 整数值（0/1/-1）。
// 为空或非法值时回退 -1。
func (a KafkaAcksConfig) Int() int {
	switch strings.ToLower(strings.TrimSpace(string(a))) {
	case "0":
		return 0
	case "1":
		return 1
	case "all", "-1", "":
		return -1
	default:
		return -1
	}
}

// IsValid 返回该 acks 配置是否是支持的语义值。
func (a KafkaAcksConfig) IsValid() bool {
	switch strings.ToLower(strings.TrimSpace(string(a))) {
	case "", "0", "1", "all", "-1":
		return true
	default:
		return false
	}
}

// StageConfig 描述 pipeline 中一个 stage 的参数集合。
type StageConfig struct {
	// Type 是 stage 名称，用于编译为具体处理函数。
	// 用法：按内置 stage 名称填写，顺序即执行顺序。
	Type string `json:"type"`
	// Offset 是偏移类 stage 的起始字节位置。
	// 用法：常与 Hex 组合，表达“从某字节位置读/写”。
	Offset int `json:"offset,omitempty"`
	// Hex 是十六进制匹配/写入参数。
	// 用法：填写无空格 hex 串，供匹配/替换类 stage 使用。
	Hex string `json:"hex,omitempty"`
	// Path 是文件语义 stage 使用的目标路径。
	// 用法：用于写文件、改路径等场景，支持绝对或相对路径。
	Path string `json:"path,omitempty"`
	// Bool 是布尔开关参数（指针用于区分未配置与 false）。
	// 用法：需要三态语义（未设/true/false）时通过该字段表达。
	Bool *bool `json:"bool,omitempty"`
	// Cases 是“匹配值(hex) -> sender 名称”的分流表。
	// 用法：用于 switch 类路由 stage，将某个固定字段映射到目标 sender。
	Cases map[string]string `json:"cases,omitempty"`
	// DefaultSender 是 switch 路由未命中时的默认 sender。
	// 用法：为空表示未命中直接丢弃（stage 返回 false）。
	DefaultSender string `json:"default_sender,omitempty"`
}

// SelectorConfig 描述一个“match key -> task set”的精确匹配器。
type SelectorConfig struct {
	// Matches 是完整 match key 到 task set 名称的静态映射。
	// 用法：key 必须由 receiver 显式构造，selector 不再解析协议语义。
	Matches map[string]string `json:"matches"`
	// DefaultTaskSet 是未命中任何规则时回退的 task set。
	// 用法：为空表示未命中直接丢弃，不再继续猜测或遍历规则。
	DefaultTaskSet string `json:"default_task_set,omitempty"`
}

// TaskConfig 描述任务绑定关系与执行模型。
type TaskConfig struct {
	// PoolSize 是任务 worker 池大小。
	// 用法：<=0 时运行时默认取 4096；CPU 富余且发送链路受限场景可调大，轻处理链路可按压测下调。
	PoolSize int `json:"pool_size"`
	// FastPath 控制是否在调用协程内同步处理（低延迟）。
	// 用法：兼容历史配置；当 ExecutionModel 为空时，FastPath=true 等价于 execution_model=fastpath。
	FastPath bool `json:"fast_path"`
	// ExecutionModel 指定任务执行模型（fastpath/pool/channel）。
	// 用法："channel" 表示单 goroutine + 有界队列顺序处理，适用于需要 task 内保序且希望避免 receiver 串行执行放大的场景。
	ExecutionModel string `json:"execution_model,omitempty"`
	// QueueSize 是任务池在“满载时允许排队等待提交”的最大长度。
	// 用法：>0 启用有界排队；<=0 时默认取 4096。该值越大，削峰能力越强但请求等待时延可能增大。
	QueueSize int `json:"queue_size,omitempty"`
	// ChannelQueueSize 是 channel 执行模型下的有界缓冲长度。
	// 用法：仅 execution_model=channel 时生效；<=0 时默认回退到 QueueSize，确保与协程池排队上限一致。
	ChannelQueueSize int `json:"channel_queue_size,omitempty"`
	// Pipelines 是按顺序执行的 pipeline 名称列表。
	// 用法：可串联多个 pipeline 形成分层处理链。
	Pipelines []string `json:"pipelines"`
	// Senders 是处理完成后要投递的发送端名称列表。
	// 用法：可配置多个 sender 实现一份输入多路分发。
	Senders []string `json:"senders"`
	// LogPayloadSend 控制该任务是否打印“发送到 sender 前”的 payload 日志。
	// 用法：用于针对单任务定位输出问题，默认关闭。
	LogPayloadSend bool `json:"log_payload_send,omitempty"`
	// PayloadLogMaxBytes 控制该任务 payload 摘要最大字节数。
	// 用法：<=0 时回退到 logging.payload_log_max_bytes。
	PayloadLogMaxBytes int `json:"payload_log_max_bytes,omitempty"`
}
