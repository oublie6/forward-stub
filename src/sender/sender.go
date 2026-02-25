// sender.go 声明发送端统一接口与工厂构建入口。
package sender

import (
	"context"

	"forword-stub/src/packet"
)

type Sender interface {
	Name() string
	Key() string
	Send(ctx context.Context, p *packet.Packet) error
	Close(ctx context.Context) error
}
