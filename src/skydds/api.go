package skydds

import "time"

const DefaultDrainBufferBytes = 4 << 20

// CommonOptions 是 Go 层传给 SkyDDS writer/reader 的最小公共配置。
// DrainBufferBytes 只被 reader drain 使用，用来限制单次 C++ bridge 拷贝到 Go 的总字节数。
type CommonOptions struct {
	DCPSConfigFile   string
	DomainID         int
	TopicName        string
	MessageModel     string
	DrainBufferBytes int
}

func normalizeDrainBufferBytes(v int) int {
	if v <= 0 {
		return DefaultDrainBufferBytes
	}
	return v
}

type Writer interface {
	// Write 发送单条 OctetMsg payload；空 payload 是否允许由具体实现决定。
	Write(payload []byte) error
	// WriteBatch 发送一批 payload，对应 BatchOctetMsg 模型。
	WriteBatch(payloads [][]byte) error
	Close() error
}

type Reader interface {
	// Wait 等待 C++ listener 通知或超时；返回 false 表示本轮没有可读数据。
	Wait(timeout time.Duration) (bool, error)
	// Drain 批量拉取已入队 payload，最多返回 maxItems 条。
	Drain(maxItems int) ([][]byte, error)
	// Poll 是兼容单条读取的辅助接口，主数据面优先使用 Wait+Drain。
	Poll(timeout time.Duration) ([]byte, error)
	// PollBatch 是兼容批量读取的辅助接口，主数据面优先使用 Wait+Drain。
	PollBatch(timeout time.Duration) ([][]byte, error)
	Close() error
}

func NewWriter(opts CommonOptions) (Writer, error) { return newWriter(opts) }
func NewReader(opts CommonOptions) (Reader, error) { return newReader(opts) }
