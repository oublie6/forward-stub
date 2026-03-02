package runtime

import (
	"context"
	"fmt"
	"testing"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

type benchCounterSender struct {
	name  string
	bytes int64
	pkts  int64
}

var _ sender.Sender = (*benchCounterSender)(nil)

func (s *benchCounterSender) Name() string { return s.name }
func (s *benchCounterSender) Key() string  { return s.name }
func (s *benchCounterSender) Send(_ context.Context, p *packet.Packet) error {
	s.pkts++
	s.bytes += int64(len(p.Payload))
	return nil
}
func (s *benchCounterSender) Close(_ context.Context) error { return nil }

func BenchmarkDispatchMatrix(b *testing.B) {
	protos := []string{"udp", "tcp", "kafka", "sftp"}
	payloadSizes := []int{256, 4096}

	for _, in := range protos {
		for _, out := range protos {
			for _, payloadSize := range payloadSizes {
				b.Run(fmt.Sprintf("%s_to_%s_%dB", in, out, payloadSize), func(b *testing.B) {
					cap := &benchCounterSender{name: out}
					tk := &task.Task{Name: "bench-task", FastPath: true, Senders: []sender.Sender{cap}}
					if err := tk.Start(); err != nil {
						b.Fatalf("task start: %v", err)
					}
					defer tk.StopGraceful()

					st := NewStore()
					st.setDispatchSubs(map[string][]*TaskState{in: []*TaskState{{Name: "bench-task", T: tk}}})
					payload := make([]byte, payloadSize)
					ctx := context.Background()

					b.SetBytes(int64(payloadSize))
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						pktPayload, rel := packet.CopyFrom(payload)
						dispatch(ctx, st, in, &packet.Packet{Envelope: packet.Envelope{Payload: pktPayload}, ReleaseFn: rel})
					}
				})
			}
		}
	}
}
