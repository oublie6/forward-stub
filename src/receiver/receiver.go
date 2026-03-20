// receiver.go 声明接收端统一接口与公共回调定义。
package receiver

import (
	"context"

	"forward-stub/src/packet"
)

type Receiver interface {
	// Name 返回配置名，供 runtime 索引、日志和聚合统计维度使用。
	Name() string
	// Key 返回能代表该 receiver 身份的稳定键，用于热重载复用/比较。
	Key() string
	// MatchKeyMode 返回当前 receiver 已编译生效的 match key 模式。
	// 该方法仅用于运行时观测、测试和热重载排障，不进入收包热路径。
	MatchKeyMode() string
	// Start 启动接收端，并在每次收到完整 packet 时调用 onPacket。
	// 实现通常会阻塞直到 ctx 取消、内部异常退出或 Stop 被调用。
	Start(ctx context.Context, onPacket func(*packet.Packet)) error
	// Stop 请求接收端停止并等待资源回收。
	Stop(ctx context.Context) error
}
