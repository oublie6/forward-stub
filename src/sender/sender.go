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
