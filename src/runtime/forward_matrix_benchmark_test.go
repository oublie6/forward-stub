// Package runtime 负责维护转发运行时对象及其测试辅助逻辑。
package runtime

import (
	"context"
	"fmt"
	"testing"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

// benchCounterSender 是基准测试中的最小发送端实现，用于统计转发次数和字节数。
type benchCounterSender struct {
	testNamedSender
	// bytes 记录累计发送字节数，用于验证吞吐统计是否符合预期。
	bytes int64
	// pkts 记录累计发送包数，便于在基准结束后核对是否丢包。
	pkts int64
}

// 确保基准发送端满足 sender.Sender 接口，避免测试辅助类型漂移。
var _ sender.Sender = (*benchCounterSender)(nil)

// Send 只累计统计信息，不执行真实网络发送。
func (s *benchCounterSender) Send(_ context.Context, p *packet.Packet) error {
	s.pkts++
	s.bytes += int64(len(p.Payload))
	return nil
}

// BenchmarkDispatchMatrix 对不同协议组合和 payload 大小的分发热路径做基准测试。
func BenchmarkDispatchMatrix(b *testing.B) {
	protos := []string{"udp", "tcp", "kafka", "sftp"}
	payloadSizes := []int{256, 4096}

	for _, in := range protos {
		for _, out := range protos {
			for _, payloadSize := range payloadSizes {
				b.Run(fmt.Sprintf("%s_to_%s_%dB", in, out, payloadSize), func(b *testing.B) {
					sinkSender := &benchCounterSender{testNamedSender: testNamedSender{name: out}}
					tk := &task.Task{Name: "bench-task", ExecutionModel: task.ExecutionModelFastPath, Senders: []sender.Sender{sinkSender}}
					if err := tk.Start(); err != nil {
						b.Fatalf("task start: %v", err)
					}
					defer tk.StopGraceful()

					st := NewStore()
					st.setDispatchSubs(map[string][]*TaskState{in: {&TaskState{Name: "bench-task", T: tk}}})
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

// BenchmarkDispatchMatrixPoolSizesNoPayloadLog 对 pool 执行模型在不同队列规模下的无日志分发性能做基准测试。
func BenchmarkDispatchMatrixPoolSizesNoPayloadLog(b *testing.B) {
	protos := []string{"udp", "tcp", "kafka", "sftp"}
	poolSizes := []int{1024, 4096, 8192}
	payloadSize := 4096

	for _, proto := range protos {
		for _, poolSize := range poolSizes {
			b.Run(fmt.Sprintf("%s_pool_%d_%dB", proto, poolSize, payloadSize), func(b *testing.B) {
				sinkSender := &benchCounterSender{testNamedSender: testNamedSender{name: proto}}
				tk := &task.Task{
					Name:           "bench-task-pool",
					ExecutionModel: task.ExecutionModelPool,
					PoolSize:       poolSize,
					Senders:        []sender.Sender{sinkSender},
				}
				if err := tk.Start(); err != nil {
					b.Fatalf("task start: %v", err)
				}
				defer tk.StopGraceful()

				st := NewStore()
				st.setDispatchSubs(map[string][]*TaskState{proto: {&TaskState{Name: "bench-task-pool", T: tk}}})
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
