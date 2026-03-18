package runtime

import (
	"context"
	"fmt"
	"testing"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

// benchCounterSender 是供 forward_matrix_benchmark_test.go 使用的包内辅助结构。
type benchCounterSender struct {
	name  string
	bytes int64
	pkts  int64
}

var _ sender.Sender = (*benchCounterSender)(nil)

// Name 提供运行时链路所需的 runtime 层行为。
func (s *benchCounterSender) Name() string { return s.name }

// Key 提供运行时链路所需的 runtime 层行为。
func (s *benchCounterSender) Key() string { return s.name }

// Send 提供运行时链路所需的 runtime 层行为。
func (s *benchCounterSender) Send(_ context.Context, p *packet.Packet) error {
	s.pkts++
	s.bytes += int64(len(p.Payload))
	return nil
}

// Close 提供运行时链路所需的 runtime 层行为。
func (s *benchCounterSender) Close(_ context.Context) error { return nil }

// BenchmarkDispatchMatrix 对 runtime 包中 DispatchMatrix 的行为进行基准测试。
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

// BenchmarkDispatchMatrixPoolSizesNoPayloadLog 对 runtime 包中 DispatchMatrixPoolSizesNoPayloadLog 的行为进行基准测试。
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
