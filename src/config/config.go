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
	DefaultPayloadLogMaxBytes      = 256
	DefaultTaskQueueSize           = 4096
)

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
	// 用法：task.Receivers 引用这里的 key，实例名建议稳定且具业务语义。
	Receivers map[string]ReceiverConfig `json:"receivers"`
	// Senders 是发送端定义表，key 为实例名。
	// 用法：task.Senders 引用这里的 key，可让多个 task 复用同一 sender 连接。
	Senders map[string]SenderConfig `json:"senders"`
	// Pipelines 是处理流水线定义表，key 为 pipeline 名称。
	// 用法：task.Pipelines 按顺序引用 pipeline 名称，从而编排处理链路。
	Pipelines map[string][]StageConfig `json:"pipelines"`
	// Tasks 是任务定义表，key 为任务名。
	// 用法：每个 task 绑定 receiver + pipeline + sender，构成一条可运行转发链路。
	Tasks map[string]TaskConfig `json:"tasks"`
}

// ControlConfig 描述远端配置中心参数。
type ControlConfig struct {
	// API 是控制面接口地址（例如 http(s)://host:port/path）。
	// 用法：配置后 runtime 会定期拉取配置；留空表示禁用远端拉取。
	API string `json:"api,omitempty"`
	// TimeoutSec 是控制面请求超时时间（秒）。
	// 用法：网络较慢场景可适度调大，避免误判拉取失败。
	TimeoutSec int `json:"timeout_sec,omitempty"`
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
	// PayloadLogTasks 是启用 payload 打印的任务名白名单。
	// 用法：仅名单内任务会打印 payload 收发日志；为空表示全部任务均不打印。
	PayloadLogTasks []string `json:"payload_log_tasks,omitempty"`
	// PayloadLogRecv 控制是否打印任务接收侧 payload 日志。
	// 用法：开启后会在 dispatch 进入任务前输出 task/receiver/元信息与 payload 摘要。
	PayloadLogRecv bool `json:"payload_log_recv,omitempty"`
	// PayloadLogSend 控制是否打印任务发送侧 payload 日志。
	// 用法：开启后会在 sender.Send 前输出 task/sender/元信息与 payload 摘要。
	PayloadLogSend bool `json:"payload_log_send,omitempty"`
	// PayloadLogMaxBytes 控制日志中 payload 摘要的最大字节数。
	// 用法：建议设置为 64~1024 之间，避免大包导致日志膨胀；<=0 时使用默认值。
	PayloadLogMaxBytes int `json:"payload_log_max_bytes,omitempty"`
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
	Multicore bool `json:"multicore"`
	// NumEventLoop 显式指定 gnet event-loop 数量（<=0 表示使用 gnet 默认值）。
	// 用法：仅对 gnet 协议有效；在 CPU 绑定压测场景可精细控制并发模型。
	NumEventLoop int `json:"num_event_loop,omitempty"`
	// ReadBufferCap 显式指定 gnet 每连接读缓冲上限（字节，<=0 表示使用 gnet 默认值）。
	// 用法：仅对 gnet 协议有效；大包场景可适度调大以减少扩容与拷贝。
	ReadBufferCap int `json:"read_buffer_cap,omitempty"`
	// Frame 是 TCP 粘包拆包规则（空或 u16be）。
	// 用法：收发两端需一致；u16be 表示以 2 字节大端长度前缀分帧。
	Frame string `json:"frame"` // "" | "u16be" (TCP)
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

	// StartOffset 指定 Kafka 启动消费位点（earliest/latest）。
	// 用法：首次启动追历史数据用 earliest，只消费新数据用 latest。
	StartOffset string `json:"start_offset,omitempty"` // earliest | latest
	// FetchMinBytes 是 Kafka 拉取最小字节数。
	// 用法：调大可提高批量效率，但会增加端到端时延。
	FetchMinBytes int `json:"fetch_min_bytes,omitempty"`
	// FetchMaxBytes 是 Kafka 单次拉取最大字节数。
	// 用法：需大于典型消息尺寸，避免频繁截断批次。
	FetchMaxBytes int `json:"fetch_max_bytes,omitempty"`
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

	// Acks 是 Kafka 生产确认策略（-1/1/0）。
	// 用法：可靠性优先选 -1，低延迟可考虑 1 或 0。
	Acks int `json:"acks,omitempty"`
	// LingerMS 是 Kafka 批发送聚合等待毫秒数。
	// 用法：调大可提高压缩和吞吐，代价是延迟上升。
	LingerMS int `json:"linger_ms,omitempty"`
	// BatchMaxBytes 是 Kafka 单批次最大字节数。
	// 用法：应与 broker 的 message.max.bytes 限制协同设置。
	BatchMaxBytes int `json:"batch_max_bytes,omitempty"`
	// Compression 是 Kafka 压缩算法（none/gzip/snappy/lz4/zstd）。
	// 用法：带宽紧张时优先启用压缩，CPU 紧张时可用 none。
	Compression string `json:"compression,omitempty"`

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
	// Flag 是标志位名称，用于条件路由 stage。
	// 用法：上游 stage 设置 flag，下游按 flag 决策分支。
	Flag string `json:"flag,omitempty"`
	// Path 是文件语义 stage 使用的目标路径。
	// 用法：用于写文件、改路径等场景，支持绝对或相对路径。
	Path string `json:"path,omitempty"`
	// Bool 是布尔开关参数（指针用于区分未配置与 false）。
	// 用法：需要三态语义（未设/true/false）时通过该字段表达。
	Bool *bool `json:"bool,omitempty"`
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
	// Receivers 是该任务订阅的接收端名称列表。
	// 用法：填写 Config.Receivers 中已定义 key，支持多源汇聚。
	Receivers []string `json:"receivers"`
	// Pipelines 是按顺序执行的 pipeline 名称列表。
	// 用法：可串联多个 pipeline 形成分层处理链。
	Pipelines []string `json:"pipelines"`
	// Senders 是处理完成后要投递的发送端名称列表。
	// 用法：可配置多个 sender 实现一份输入多路分发。
	Senders []string `json:"senders"`
	// LogPayloadRecv 控制该任务是否打印“接收到任务前”的 payload 日志（需 logging.PayloadLogRecv=true 且命中白名单）。
	// 用法：用于针对单任务定位输入问题，默认关闭。
	LogPayloadRecv bool `json:"log_payload_recv,omitempty"`
	// LogPayloadSend 控制该任务是否打印“发送到 sender 前”的 payload 日志（需 logging.PayloadLogSend=true 且命中白名单）。
	// 用法：用于针对单任务定位输出问题，默认关闭。
	LogPayloadSend bool `json:"log_payload_send,omitempty"`
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
	for name, tc := range c.Tasks {
		if tc.QueueSize <= 0 {
			tc.QueueSize = DefaultTaskQueueSize
		}
		if tc.ChannelQueueSize <= 0 {
			tc.ChannelQueueSize = tc.QueueSize
		}
		c.Tasks[name] = tc
	}
}
