// Package runtime 负责维护转发运行时对象及其测试辅助逻辑。
package runtime

import (
	"context"
	"fmt"
	"hash/fnv"
	"path"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"forward-stub/src/packet"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

// benchScenario 描述一组端到端基准场景，包括入站构包方式和下游 sender 构造策略。
type benchScenario struct {
	// name 是基准子场景名称，会直接出现在 benchmark 输出中。
	name string
	// receiver 是测试时写入 Store 的 receiver 名称，用于命中对应的分发表项。
	receiver string
	// makePkt 根据序号和 payload 构造输入包，用于模拟不同协议的入站语义。
	makePkt func(seq int, payload []byte) *packet.Packet
	// mkSender 根据并发度和目标数构造下游 sender，用于比较不同出站模型。
	mkSender func(concurrency, targets int) sender.Sender
}

// benchUDPDownstreamSender 模拟 UDP 下游发送端，仅做分片和目标轮询，不执行真实网络 IO。
type benchUDPDownstreamSender struct {
	testNamedSender
	// shardMask 用于把递增序号映射到固定分片；要求并发度满足 2 的幂时效果最佳。
	shardMask int
	// nextShard 保存全局递增序号，供多协程并发取模分片。
	nextShard atomic.Uint64
	// rr 为每个分片维护独立轮询计数，避免所有分片争用同一个原子变量。
	rr []atomic.Uint64
	// targets 保存可选下游地址，仅用于模拟多目标场景下的轮询开销。
	targets []string
}

// Send 模拟按分片轮询目标地址的计算成本，不进行真实发送。
func (s *benchUDPDownstreamSender) Send(_ context.Context, p *packet.Packet) error {
	idx := int(nextShardIndex(&s.nextShard, s.shardMask))
	targetIdx := 0
	if len(s.targets) > 1 {
		targetIdx = int(s.rr[idx].Add(1)-1) % len(s.targets)
	}
	_ = s.targets[targetIdx]
	_ = len(p.Payload)
	return nil
}

// benchTCPDownstreamSender 模拟 TCP 下游 sender，可选是否做长度前缀封包。
type benchTCPDownstreamSender struct {
	testNamedSender
	// withLen 表示是否模拟 u16be 长度头编码；为 false 时等价于裸流模式。
	withLen bool
	// shardMask 用于把递增序号映射到固定分片；要求并发度满足 2 的幂时效果最佳。
	shardMask int
	// nextShard 保存全局递增序号，供多协程并发取模分片。
	nextShard atomic.Uint64
	// framePool 复用封包缓冲区，避免基准结果被重复分配放大。
	framePool sync.Pool
}

// Send 模拟 TCP sender 的分片选择和可选的长度前缀封包成本。
func (s *benchTCPDownstreamSender) Send(_ context.Context, p *packet.Packet) error {
	_ = int(nextShardIndex(&s.nextShard, s.shardMask))
	if !s.withLen {
		return nil
	}
	bufPtr := s.framePool.Get().(*[]byte)
	n := len(p.Payload)
	buf := *bufPtr
	if cap(buf) < 2+n {
		buf = make([]byte, 2+n)
	} else {
		buf = buf[:2+n]
	}
	buf[0] = byte(n >> 8)
	buf[1] = byte(n)
	copy(buf[2:], p.Payload)
	*bufPtr = buf[:0]
	s.framePool.Put(bufPtr)
	return nil
}

// benchKafkaDownstreamSender 模拟 Kafka sender 的分片、topic 和 header 处理开销。
type benchKafkaDownstreamSender struct {
	testNamedSender
	// topic 保存目标 topic，仅用于模拟访问成本。
	topic string
	// shardMask 用于把递增序号映射到固定分片；要求并发度满足 2 的幂时效果最佳。
	shardMask int
	// nextShard 保存全局递增序号，供多协程并发取模分片。
	nextShard atomic.Uint64
	// headers 保存模拟消息头，用于覆盖 header 复制/访问场景。
	headers [][2]string
}

// Send 模拟 Kafka sender 在写入前对 topic、header 与 payload 元信息的访问成本。
func (s *benchKafkaDownstreamSender) Send(_ context.Context, p *packet.Packet) error {
	_ = int(nextShardIndex(&s.nextShard, s.shardMask))
	_ = s.topic
	_ = p.Meta.TransferID
	_ = s.headers
	_ = len(p.Payload)
	return nil
}

// benchSFTPDownstreamSender 模拟 SFTP sender 的分块落盘过程，并维护每个传输的写入进度。
type benchSFTPDownstreamSender struct {
	testNamedSender
	// remoteDir 是模拟远端目录，用于构造最终路径和临时文件路径。
	remoteDir string
	// shardMask 用于把递增序号映射到固定分片；要求并发度满足 2 的幂时效果最佳。
	shardMask int
	// nextShard 保存全局递增序号，供多协程并发取模分片。
	nextShard atomic.Uint64
	// mu 保护 written 映射，避免并发更新相同 transferID 时数据竞争。
	mu sync.Mutex
	// written 记录每个 transferID 已写入到的最新偏移，用于模拟续传进度。
	written map[string]int64
}

// Send 模拟 SFTP 分块写入和 EOF 清理状态的成本。
func (s *benchSFTPDownstreamSender) Send(_ context.Context, p *packet.Packet) error {
	_ = int(nextShardIndex(&s.nextShard, s.shardMask))
	transferID := p.Meta.TransferID
	if transferID == "" {
		transferID = "stream"
	}
	finalPath := path.Join(s.remoteDir, p.Meta.FileName)
	tempPath := finalPath + ".part"
	_ = tempPath
	s.mu.Lock()
	s.written[transferID] = p.Meta.Offset + int64(len(p.Payload))
	if p.Meta.EOF {
		delete(s.written, transferID)
	}
	s.mu.Unlock()
	return nil
}

// nextShardIndex 依据递增序号和掩码选择分片索引；mask<=0 时始终返回 0。
func nextShardIndex(next *atomic.Uint64, mask int) int {
	if mask <= 0 {
		return 0
	}
	i := int(next.Add(1)-1) & mask
	return i
}

// benchmarkTask 根据执行模型构造并启动一个基准任务，并返回对应清理函数。
func benchmarkTask(model string, snd sender.Sender) (*task.Task, func()) {
	tk := &task.Task{
		Name:             "bench-task",
		ExecutionModel:   model,
		FastPath:         model == task.ExecutionModelFastPath,
		PoolSize:         1024,
		ChannelQueueSize: 1024,
		Senders:          []sender.Sender{snd},
	}
	if err := tk.Start(); err != nil {
		panic(err)
	}
	cleanup := func() { tk.StopGraceful() }
	return tk, cleanup
}

// makeDispatchStore 为单 receiver 场景构造测试 Store，避免每个子基准重复拼装快照。
func makeDispatchStore(receiverName string, tk *task.Task) *Store {
	st := NewStore()
	st.setDispatchSubs(map[string][]*TaskState{receiverName: []*TaskState{{Name: "bench-task", T: tk}}})
	return st
}

// ingestUDPDatagram 构造一个模拟 UDP 数据报输入包。
func ingestUDPDatagram(payload []byte, remote, local string) *packet.Packet {
	buf, rel := packet.CopyFrom(payload)
	return &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindStream, Payload: buf, Meta: packet.Meta{Proto: packet.ProtoUDP, Remote: remote, Local: local}}, ReleaseFn: rel}
}

// ingestTCPChunk 构造一个模拟 TCP 流分片输入包。
func ingestTCPChunk(payload []byte, remote, local string) *packet.Packet {
	buf, rel := packet.CopyFrom(payload)
	return &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindStream, Payload: buf, Meta: packet.Meta{Proto: packet.ProtoTCP, Remote: remote, Local: local}}, ReleaseFn: rel}
}

// ingestKafkaRecord 构造一个模拟 Kafka 记录输入包。
func ingestKafkaRecord(value []byte, topic, groupID string) *packet.Packet {
	buf, rel := packet.CopyFrom(value)
	return &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindStream, Payload: buf, Meta: packet.Meta{Proto: packet.ProtoKafka, Remote: topic, Local: groupID}}, ReleaseFn: rel}
}

// ingestSFTPChunk 构造一个模拟 SFTP 文件分块输入包。
func ingestSFTPChunk(chunk []byte, transferID string, offset int64, totalSize int64, eof bool) *packet.Packet {
	buf, rel := packet.CopyFrom(chunk)
	return &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindFileChunk, Payload: buf, Meta: packet.Meta{Proto: packet.ProtoSFTP, Remote: "/in/file.bin", Local: "sftp:22", FileName: "file.bin", FilePath: "/in/file.bin", TransferID: transferID, Offset: offset, TotalSize: totalSize, EOF: eof}}, ReleaseFn: rel}
}

// scenarioBenchmarks 返回所有协议组合基准场景，集中维护便于扩展和复用。
func scenarioBenchmarks() []benchScenario {
	return []benchScenario{
		{
			name:     "UDP_to_UDP",
			receiver: "udp-rx",
			makePkt: func(seq int, payload []byte) *packet.Packet {
				return ingestUDPDatagram(payload, "10.0.0.1:12000", "10.0.0.2:13000")
			},
			mkSender: func(concurrency, targets int) sender.Sender {
				t := make([]string, targets)
				for i := range t {
					t[i] = "10.0.1." + strconv.Itoa(i+1) + ":9000"
				}
				return &benchUDPDownstreamSender{testNamedSender: testNamedSender{name: "udp", key: "bench_udp|udp"}, shardMask: concurrency - 1, targets: t, rr: make([]atomic.Uint64, concurrency)}
			},
		},
		{
			name:     "UDP_to_TCP",
			receiver: "udp-rx",
			makePkt: func(seq int, payload []byte) *packet.Packet {
				return ingestUDPDatagram(payload, "10.0.0.1:12000", "10.0.0.2:13000")
			},
			mkSender: func(concurrency, targets int) sender.Sender {
				return &benchTCPDownstreamSender{testNamedSender: testNamedSender{name: "tcp", key: "bench_tcp|tcp"}, withLen: true, shardMask: concurrency - 1, framePool: sync.Pool{New: func() any { b := make([]byte, 0, 2048); return &b }}}
			},
		},
		{
			name:     "TCP_to_UDP",
			receiver: "tcp-rx",
			makePkt: func(seq int, payload []byte) *packet.Packet {
				return ingestTCPChunk(payload, "10.1.0.1:14000", "10.1.0.2:15000")
			},
			mkSender: func(concurrency, targets int) sender.Sender {
				t := make([]string, targets)
				for i := range t {
					t[i] = "10.0.2." + strconv.Itoa(i+1) + ":9100"
				}
				return &benchUDPDownstreamSender{testNamedSender: testNamedSender{name: "udp", key: "bench_udp|udp"}, shardMask: concurrency - 1, targets: t, rr: make([]atomic.Uint64, concurrency)}
			},
		},
		{
			name:     "Kafka_to_UDP",
			receiver: "kafka-rx",
			makePkt: func(seq int, payload []byte) *packet.Packet {
				return ingestKafkaRecord(payload, "topic.bench", "group.bench")
			},
			mkSender: func(concurrency, targets int) sender.Sender {
				t := make([]string, targets)
				for i := range t {
					t[i] = "10.0.3." + strconv.Itoa(i+1) + ":9200"
				}
				return &benchUDPDownstreamSender{testNamedSender: testNamedSender{name: "udp", key: "bench_udp|udp"}, shardMask: concurrency - 1, targets: t, rr: make([]atomic.Uint64, concurrency)}
			},
		},
		{
			name:     "UDP_to_Kafka",
			receiver: "udp-rx",
			makePkt: func(seq int, payload []byte) *packet.Packet {
				return ingestUDPDatagram(payload, "10.2.0.1:12000", "10.2.0.2:13000")
			},
			mkSender: func(concurrency, targets int) sender.Sender {
				return &benchKafkaDownstreamSender{testNamedSender: testNamedSender{name: "kafka", key: "bench_kafka|kafka"}, topic: "topic.out", shardMask: concurrency - 1, headers: [][2]string{{"x-scenario", "udp_to_kafka"}}}
			},
		},
		{
			name:     "SFTP_to_TCP",
			receiver: "sftp-rx",
			makePkt: func(seq int, payload []byte) *packet.Packet {
				id := "transfer-" + strconv.Itoa(seq&255)
				offset := int64((seq & 63) * len(payload))
				total := int64(64 * len(payload))
				eof := seq%64 == 63
				return ingestSFTPChunk(payload, id, offset, total, eof)
			},
			mkSender: func(concurrency, targets int) sender.Sender {
				return &benchTCPDownstreamSender{testNamedSender: testNamedSender{name: "tcp", key: "bench_tcp|tcp"}, withLen: true, shardMask: concurrency - 1, framePool: sync.Pool{New: func() any { b := make([]byte, 0, 2048); return &b }}}
			},
		},
		{
			name:     "TCP_to_SFTP",
			receiver: "tcp-rx",
			makePkt: func(seq int, payload []byte) *packet.Packet {
				pkt := ingestTCPChunk(payload, "10.3.0.1:14000", "10.3.0.2:15000")
				pkt.Envelope.Kind = packet.PayloadKindFileChunk
				pkt.Envelope.Meta.FileName = "file.bin"
				pkt.Envelope.Meta.TransferID = "tcp-sftp-" + strconv.Itoa(seq&255)
				pkt.Envelope.Meta.Offset = int64((seq & 63) * len(payload))
				pkt.Envelope.Meta.EOF = seq%64 == 63
				return pkt
			},
			mkSender: func(concurrency, targets int) sender.Sender {
				return &benchSFTPDownstreamSender{testNamedSender: testNamedSender{name: "sftp", key: "bench_sftp|sftp"}, remoteDir: "/out", shardMask: concurrency - 1, written: make(map[string]int64)}
			},
		},
	}
}

// BenchmarkScenarioForwarding 对多协议、多 payload、多并发和多目标组合做综合转发基准测试。
func BenchmarkScenarioForwarding(b *testing.B) {
	scenarios := scenarioBenchmarks()
	payloadSizes := []int{64, 128, 512, 1200, 4096}
	concurrencies := []int{1, 2, 4, 8}
	targetCounts := []int{1, 4}
	executionModels := []string{task.ExecutionModelFastPath}
	ctx := context.Background()

	for _, sc := range scenarios {
		sc := sc
		b.Run(sc.name, func(b *testing.B) {
			for _, model := range executionModels {
				for _, payloadSize := range payloadSizes {
					for _, concurrency := range concurrencies {
						for _, targetCount := range targetCounts {
							name := fmt.Sprintf("model=%s/payload=%dB/sender_conc=%d/targets=%d", model, payloadSize, concurrency, targetCount)
							b.Run(name, func(b *testing.B) {
								payload := make([]byte, payloadSize)
								h := fnv.New64a()
								_, _ = h.Write([]byte(sc.name))
								seed := h.Sum64()
								for i := range payload {
									payload[i] = byte((seed + uint64(i)) & 0xff)
								}

								snd := sc.mkSender(concurrency, targetCount)
								tk, cleanupTask := benchmarkTask(model, snd)
								defer cleanupTask()
								st := makeDispatchStore(sc.receiver, tk)

								b.SetBytes(int64(payloadSize))
								b.ReportAllocs()
								b.ResetTimer()
								for i := 0; i < b.N; i++ {
									pkt := sc.makePkt(i, payload)
									dispatch(ctx, st, sc.receiver, pkt)
								}
							})
						}
					}
				}
			}
		})
	}
}
