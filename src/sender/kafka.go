package sender

import (
	"context"
	"errors"

	"forword-stub/src/packet"
)

type KafkaSender struct {
	name  string
	topic string
}

func NewKafkaSender(name, topic string) *KafkaSender {
	return &KafkaSender{name: name, topic: topic}
}

func (s *KafkaSender) Name() string { return s.name }
func (s *KafkaSender) Key() string  { return "kafka|" + s.topic }

func (s *KafkaSender) Send(ctx context.Context, p *packet.Packet) error {
	return errors.New("kafka sender not implemented")
}

func (s *KafkaSender) Close(ctx context.Context) error { return nil }
