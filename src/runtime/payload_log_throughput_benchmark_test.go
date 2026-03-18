// payload_log_throughput_benchmark_test.go 关注 payload 日志开关对 dispatch/task 热路径的影响。
package runtime

import (
	"context"
	"fmt"
	"testing"

	"forward-stub/src/logx"
	"forward-stub/src/packet"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

// BenchmarkPayloadLogSwitchThroughput 对 udp/tcp/kafka 转发任务在 payload 日志开关场景下进行吞吐压测，
// 并在每个子用例结束时校验发送计数，确保测试链路中不丢包。
func BenchmarkPayloadLogSwitchThroughput(b *testing.B) {
	if err := logx.Init(logx.Options{Level: "error"}); err != nil {
		b.Fatalf("log init: %v", err)
	}
	defer func() { _ = logx.Sync() }()

	protos := []string{"udp", "tcp", "kafka"}
	payloadSizes := []int{256, 4096}

	for _, proto := range protos {
		for _, payloadSize := range payloadSizes {
			for _, payloadLogEnabled := range []bool{false, true} {
				caseName := fmt.Sprintf("%s_payloadlog_%t_%dB", proto, payloadLogEnabled, payloadSize)
				b.Run(caseName, func(b *testing.B) {
					sinkSender := &benchCounterSender{name: proto + "-sink"}
					tk := &task.Task{
						Name:           "bench-" + proto,
						FastPath:       true,
						Senders:        []sender.Sender{sinkSender},
						LogPayloadSend: payloadLogEnabled,
						PayloadLogMax:  128,
					}
					if err := tk.Start(); err != nil {
						b.Fatalf("task start: %v", err)
					}
					defer tk.StopGraceful()

					st := NewStore()
					st.setDispatchSubs(testDispatchSnapshot(proto, &TaskState{Name: tk.Name, T: tk}))

					ctx := context.Background()
					payload := make([]byte, payloadSize)

					b.SetBytes(int64(payloadSize))
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						pktPayload, rel := packet.CopyFrom(payload)
						dispatch(ctx, st, proto, &packet.Packet{Envelope: packet.Envelope{Payload: pktPayload}, ReleaseFn: rel})
					}
					b.StopTimer()

					if sinkSender.pkts != int64(b.N) {
						b.Fatalf("packet loss detected: sent=%d received=%d", b.N, sinkSender.pkts)
					}
				})
			}
		}
	}
}
