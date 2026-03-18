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

type benchScenario struct {
	name     string
	receiver string
	makePkt  func(seq int, payload []byte) *packet.Packet
	mkSender func(concurrency, targets int) sender.Sender
}

type benchUDPDownstreamSender struct {
	name      string
	shardMask int
	nextShard atomic.Uint64
	rr        []atomic.Uint64
	targets   []string
}

func (s *benchUDPDownstreamSender) Name() string                { return s.name }
func (s *benchUDPDownstreamSender) Key() string                 { return "bench_udp|" + s.name }
func (s *benchUDPDownstreamSender) Close(context.Context) error { return nil }
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

type benchTCPDownstreamSender struct {
	name      string
	withLen   bool
	shardMask int
	nextShard atomic.Uint64
	framePool sync.Pool
}

func (s *benchTCPDownstreamSender) Name() string                { return s.name }
func (s *benchTCPDownstreamSender) Key() string                 { return "bench_tcp|" + s.name }
func (s *benchTCPDownstreamSender) Close(context.Context) error { return nil }
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

type benchKafkaDownstreamSender struct {
	name      string
	topic     string
	shardMask int
	nextShard atomic.Uint64
	headers   [][2]string
}

func (s *benchKafkaDownstreamSender) Name() string                { return s.name }
func (s *benchKafkaDownstreamSender) Key() string                 { return "bench_kafka|" + s.name }
func (s *benchKafkaDownstreamSender) Close(context.Context) error { return nil }
func (s *benchKafkaDownstreamSender) Send(_ context.Context, p *packet.Packet) error {
	_ = int(nextShardIndex(&s.nextShard, s.shardMask))
	_ = s.topic
	_ = p.Meta.TransferID
	_ = s.headers
	_ = len(p.Payload)
	return nil
}

type benchSFTPDownstreamSender struct {
	name      string
	remoteDir string
	shardMask int
	nextShard atomic.Uint64
	mu        sync.Mutex
	written   map[string]int64
}

func (s *benchSFTPDownstreamSender) Name() string                { return s.name }
func (s *benchSFTPDownstreamSender) Key() string                 { return "bench_sftp|" + s.name }
func (s *benchSFTPDownstreamSender) Close(context.Context) error { return nil }
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

func nextShardIndex(next *atomic.Uint64, mask int) int {
	if mask <= 0 {
		return 0
	}
	i := int(next.Add(1)-1) & mask
	return i
}

func benchmarkTask(model string, snd sender.Sender) (*task.Task, func()) {
	tk := &task.Task{
		Name:             "bench-task",
		ExecutionModel:   model,
		FastPath:         model == task.ExecutionModelFastPath,
		PoolSize:         1024,
		QueueSize:        1024,
		ChannelQueueSize: 1024,
		Senders:          []sender.Sender{snd},
	}
	if err := tk.Start(); err != nil {
		panic(err)
	}
	cleanup := func() { tk.StopGraceful() }
	return tk, cleanup
}

func makeDispatchStore(receiverName string, tk *task.Task) *Store {
	st := NewStore()
	st.setDispatchSubs(testDispatchSnapshot(receiverName, &TaskState{Name: "bench-task", T: tk}))
	return st
}

func ingestUDPDatagram(payload []byte, remote, local string) *packet.Packet {
	buf, rel := packet.CopyFrom(payload)
	return &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindStream, Payload: buf, Meta: packet.Meta{Proto: packet.ProtoUDP, Remote: remote, Local: local}}, ReleaseFn: rel}
}

func ingestTCPChunk(payload []byte, remote, local string) *packet.Packet {
	buf, rel := packet.CopyFrom(payload)
	return &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindStream, Payload: buf, Meta: packet.Meta{Proto: packet.ProtoTCP, Remote: remote, Local: local}}, ReleaseFn: rel}
}

func ingestKafkaRecord(value []byte, topic, groupID string) *packet.Packet {
	buf, rel := packet.CopyFrom(value)
	return &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindStream, Payload: buf, Meta: packet.Meta{Proto: packet.ProtoKafka, Remote: topic, Local: groupID}}, ReleaseFn: rel}
}

func ingestSFTPChunk(chunk []byte, transferID string, offset int64, totalSize int64, eof bool) *packet.Packet {
	buf, rel := packet.CopyFrom(chunk)
	return &packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindFileChunk, Payload: buf, Meta: packet.Meta{Proto: packet.ProtoSFTP, Remote: "/in/file.bin", Local: "sftp:22", FileName: "file.bin", FilePath: "/in/file.bin", TransferID: transferID, Offset: offset, TotalSize: totalSize, EOF: eof}}, ReleaseFn: rel}
}

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
				return &benchUDPDownstreamSender{name: "udp", shardMask: concurrency - 1, targets: t, rr: make([]atomic.Uint64, concurrency)}
			},
		},
		{
			name:     "UDP_to_TCP",
			receiver: "udp-rx",
			makePkt: func(seq int, payload []byte) *packet.Packet {
				return ingestUDPDatagram(payload, "10.0.0.1:12000", "10.0.0.2:13000")
			},
			mkSender: func(concurrency, targets int) sender.Sender {
				return &benchTCPDownstreamSender{name: "tcp", withLen: true, shardMask: concurrency - 1, framePool: sync.Pool{New: func() any { b := make([]byte, 0, 2048); return &b }}}
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
				return &benchUDPDownstreamSender{name: "udp", shardMask: concurrency - 1, targets: t, rr: make([]atomic.Uint64, concurrency)}
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
				return &benchUDPDownstreamSender{name: "udp", shardMask: concurrency - 1, targets: t, rr: make([]atomic.Uint64, concurrency)}
			},
		},
		{
			name:     "UDP_to_Kafka",
			receiver: "udp-rx",
			makePkt: func(seq int, payload []byte) *packet.Packet {
				return ingestUDPDatagram(payload, "10.2.0.1:12000", "10.2.0.2:13000")
			},
			mkSender: func(concurrency, targets int) sender.Sender {
				return &benchKafkaDownstreamSender{name: "kafka", topic: "topic.out", shardMask: concurrency - 1, headers: [][2]string{{"x-scenario", "udp_to_kafka"}}}
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
				return &benchTCPDownstreamSender{name: "tcp", withLen: true, shardMask: concurrency - 1, framePool: sync.Pool{New: func() any { b := make([]byte, 0, 2048); return &b }}}
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
				return &benchSFTPDownstreamSender{name: "sftp", remoteDir: "/out", shardMask: concurrency - 1, written: make(map[string]int64)}
			},
		},
	}
}

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
