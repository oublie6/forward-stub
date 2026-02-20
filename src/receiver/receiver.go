package receiver

import (
	"context"

	"forword-stub/src/packet"
)

type Receiver interface {
	Name() string
	Key() string
	Start(ctx context.Context, onPacket func(*packet.Packet)) error
	Stop(ctx context.Context) error
}
