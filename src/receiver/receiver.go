// receiver.go 声明接收端统一接口与公共回调定义。
package receiver

import (
	"context"

	"forward-stub/src/packet"
)

// Receiver 描述转发架构中 receiver 层的状态。
type Receiver interface {
	Name() string
	Key() string
	Start(ctx context.Context, onPacket func(*packet.Packet)) error
	Stop(ctx context.Context) error
}
