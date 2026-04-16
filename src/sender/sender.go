// sender.go 声明发送端统一接口与工厂构建入口。
package sender

import (
	"context"

	"forward-stub/src/packet"
)

type Sender interface {
	Name() string
	Key() string
	Send(ctx context.Context, p *packet.Packet) error
	Close(ctx context.Context) error
}

// AsyncRuntimeStats 描述 sender 内部异步队列的运行时快照。
// 这是可选接口使用的数据模型，不改变 Sender 主接口的同步形态。
type AsyncRuntimeStats struct {
	QueueSize      int
	QueueUsed      int
	QueueAvailable int
	Dropped        uint64
	SendErrors     uint64
	SendSuccess    uint64
}

// AsyncRuntimeStatsProvider 由支持本地异步队列的 sender 选择性实现。
// task 聚合统计会通过该接口采样队列水位和 callback 发送结果。
type AsyncRuntimeStatsProvider interface {
	AsyncRuntimeStats() AsyncRuntimeStats
}
