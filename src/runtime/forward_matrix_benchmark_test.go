package runtime

import (
	"context"
	"fmt"
	"testing"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

// benchCounterSender stores package-local state used by forward_matrix_benchmark_test.go.
type benchCounterSender struct {
	name  string
	bytes int64
	pkts  int64
}

var _ sender.Sender = (*benchCounterSender)(nil)

// Name provides runtime-level behavior used by the runtime pipeline.
func (s *benchCounterSender) Name() string { return s.name }

// Key provides runtime-level behavior used by the runtime pipeline.
func (s *benchCounterSender) Key() string { return s.name }

// Send provides runtime-level behavior used by the runtime pipeline.
func (s *benchCounterSender) Send(_ context.Context, p *packet.Packet) error {
	s.pkts++
	s.bytes += int64(len(p.Payload))
	return nil
}

// Close provides runtime-level behavior used by the runtime pipeline.
func (s *benchCounterSender) Close(_ context.Context) error { return nil }

// BenchmarkDispatchMatrix benchmarks the DispatchMatrix behavior for the runtime package.
func BenchmarkDispatchMatrix(b *testing.B) {
	protos := []string{"udp", "tcp", "kafka", "sftp"}
	payloadSizes := []int{256, 4096}

	for _, in := range protos {
		for _, out := range protos {
			for _, payloadSize := range payloadSizes {
				b.Run(fmt.Sprintf("%s_to_%s_%dB", in, out, payloadSize), func(b *testing.B) {
					sinkSender := &benchCounterSender{name: out}
					tk := &task.Task{Name: "bench-task", FastPath: true, Senders: []sender.Sender{sinkSender}}
					if err := tk.Start(); err != nil {
						b.Fatalf("task start: %v", err)
					}
					defer tk.StopGraceful()

					st := NewStore()
					st.setDispatchSubs(testDispatchSnapshot(in, &TaskState{Name: "bench-task", T: tk}))
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

// BenchmarkDispatchMatrixPoolSizesNoPayloadLog benchmarks the DispatchMatrixPoolSizesNoPayloadLog behavior for the runtime package.
func BenchmarkDispatchMatrixPoolSizesNoPayloadLog(b *testing.B) {
	protos := []string{"udp", "tcp", "kafka", "sftp"}
	poolSizes := []int{1024, 4096, 8192}
	payloadSize := 4096

	for _, proto := range protos {
		for _, poolSize := range poolSizes {
			b.Run(fmt.Sprintf("%s_pool_%d_%dB", proto, poolSize, payloadSize), func(b *testing.B) {
				sinkSender := &benchCounterSender{name: proto}
				tk := &task.Task{
					Name:           "bench-task-pool",
					ExecutionModel: task.ExecutionModelPool,
					PoolSize:       poolSize,
					QueueSize:      poolSize,
					Senders:        []sender.Sender{sinkSender},
				}
				if err := tk.Start(); err != nil {
					b.Fatalf("task start: %v", err)
				}
				defer tk.StopGraceful()

				st := NewStore()
				st.setDispatchSubs(testDispatchSnapshot(proto, &TaskState{Name: "bench-task-pool", T: tk}))
				payload := make([]byte, payloadSize)
				ctx := context.Background()

				b.SetBytes(int64(payloadSize))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					pktPayload, rel := packet.CopyFrom(payload)
					dispatch(ctx, st, proto, &packet.Packet{Envelope: packet.Envelope{Payload: pktPayload}, ReleaseFn: rel})
				}
			})
		}
	}
}
