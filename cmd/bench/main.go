// Package main provides a self-contained benchmark harness for forward-stub.
//
// 设计目标：
//  1. 在单进程内构建“generator -> receiver -> dispatch -> task -> sender -> sink”最小闭环；
//  2. 量化 UDP/TCP 路径的基础吞吐与丢包趋势；
//  3. 对比 task 执行模型（fastpath/pool/channel）和关键参数的影响；
//  4. 作为代码改动后的快速回归工具。
//
// 覆盖范围：
//  - 覆盖 runtime.UpdateCache 构建路径、dispatch 分发、task 调度和 UDP/TCP sender 写出；
//  - 默认 pipeline 为空，主要测调度与发送路径固定成本；
//  - 不直接覆盖 Kafka/SFTP 外部依赖、跨主机网络抖动和生产部署拓扑。
//
// 结果解读：
//  - 更适用于“同机同参数”的横向对比，不应直接等同生产容量承诺；
//  - `both` 模式是顺序执行 udp 与 tcp 场景，不是并行混压。
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"forward-stub/src/app"
	"forward-stub/src/config"
	"forward-stub/src/logx"
)

type metrics struct {
	sentPackets uint64
	sentBytes   uint64
	recvPackets uint64
	recvBytes   uint64
	orderErrors uint64
	expectedSeq uint64
	seqInit     uint32
	seqCheck    bool
	seqAlloc    uint64
}

// result 是单轮 benchmark 的最终统计快照。
//
// 注意：
//  - sent/recv 统计在 warmup 后采样，尽量避免启动抖动干扰；
//  - pps 与 mbps 基于 recv 侧计算，反映有效输出吞吐。
type result struct {
	proto          string
	duration       time.Duration
	payloadSize    int
	senderWorkers  int
	sentPackets    uint64
	sentBytes      uint64
	recvPackets    uint64
	recvBytes      uint64
	packetLossRate float64
	orderErrors    uint64
	strictOrderOK  bool
	pps            float64
	mbps           float64
}

// benchFileConfig 对应 -bench-config JSON 文件。
//
// 使用指引：
//  - 所有字段均为可选，只有显式出现才覆盖命令行默认值；
//  - 保持与 flag 名语义一致，降低“配置文件模式”和“命令行模式”之间的心智切换成本。
type benchFileConfig struct {
	Mode                 string `json:"mode,omitempty"`
	Duration             string `json:"duration,omitempty"`
	Warmup               string `json:"warmup,omitempty"`
	PayloadSize          *int   `json:"payload_size,omitempty"`
	Workers              *int   `json:"workers,omitempty"`
	PPSPerWorker         *int   `json:"pps_per_worker,omitempty"`
	PPSSweep             string `json:"pps_sweep,omitempty"`
	Multicore            *bool  `json:"multicore,omitempty"`
	UDPSinkReaders       *int   `json:"udp_sink_readers,omitempty"`
	UDPSinkReadBuf       *int   `json:"udp_sink_read_buf,omitempty"`
	TaskFastPath         *bool  `json:"task_fast_path,omitempty"`
	TaskPoolSize         *int   `json:"task_pool_size,omitempty"`
	TaskQueueSize        *int   `json:"task_queue_size,omitempty"`
	TaskChannelQueueSize *int   `json:"task_channel_queue_size,omitempty"`
	TaskExecutionModel   string `json:"task_execution_model,omitempty"`
	ReceiverEventLoops   *int   `json:"receiver_event_loops,omitempty"`
	ReceiverReadBuffer   *int   `json:"receiver_read_buffer_cap,omitempty"`
	TCPSenderConcurrency *int   `json:"tcp_sender_concurrency,omitempty"`
	BasePort             *int   `json:"base_port,omitempty"`
	LogLevel             string `json:"log_level,omitempty"`
	LogFile              string `json:"log_file,omitempty"`
	TrafficStatsInterval string `json:"traffic_stats_interval,omitempty"`
	ValidateOrder        *bool  `json:"validate_order,omitempty"`
	PipelineProfile      string `json:"pipeline_profile,omitempty"`
}

// main 负责参数解析、场景展开、执行与结果输出。
//
// 关键流程：
//  1. 解析命令行参数并可选加载 bench-config 覆盖；
//  2. 做输入合法性校验（如 payload-size、validate-order）；
//  3. 根据 pps-sweep 形成测试档位，按 mode 顺序运行；
//  4. 每个场景调用 runForwardBenchmark 获取结果并打印。
func main() {
	benchConfigPath := flag.String("bench-config", "", "benchmark config json path (optional)")
	mode := flag.String("mode", "both", "benchmark mode: udp|tcp|both")
	duration := flag.Duration("duration", 8*time.Second, "measure duration")
	warmup := flag.Duration("warmup", 2*time.Second, "warmup duration before measure")
	payloadSize := flag.Int("payload-size", 512, "payload size in bytes")
	workers := flag.Int("workers", max(1, runtime.NumCPU()/2), "number of generator workers")
	ppsPerWorker := flag.Int("pps-per-worker", 0, "send rate limit per worker (0 means unbounded)")
	ppsSweep := flag.String("pps-sweep", "", "comma-separated pps-per-worker list, e.g. 1000,2000,4000")
	multicore := flag.Bool("multicore", true, "whether receivers use gnet multicore")
	udpSinkReaders := flag.Int("udp-sink-readers", max(1, runtime.NumCPU()/2), "number of concurrent UDP sink readers")
	udpSinkReadBuf := flag.Int("udp-sink-read-buf", 16<<20, "UDP sink socket read buffer bytes")
	taskFastPath := flag.Bool("task-fast-path", false, "whether benchmark task uses fast_path")
	taskPoolSize := flag.Int("task-pool-size", 2048, "benchmark task worker pool size when execution_model=pool")
	taskQueueSize := flag.Int("task-queue-size", 4096, "benchmark task queue size when execution_model=pool")
	taskChannelQueueSize := flag.Int("task-channel-queue-size", 0, "benchmark task channel queue size when execution_model=channel (<=0 means fallback to queue_size)")
	taskExecutionModel := flag.String("task-execution-model", "", "task execution model: fastpath|pool|channel (empty means derive from task-fast-path)")
	receiverEventLoops := flag.Int("receiver-event-loops", runtime.NumCPU(), "receiver gnet num_event_loop")
	receiverReadBufferCap := flag.Int("receiver-read-buffer-cap", 0, "receiver read buffer cap bytes (0 means use default)")
	tcpSenderConcurrency := flag.Int("tcp-sender-concurrency", 4, "tcp sender internal connection concurrency")
	basePort := flag.Int("base-port", 0, "benchmark receiver base port (0 uses default by proto: udp=19100,tcp=19200)")
	logLevel := flag.String("log-level", "warn", "benchmark runtime log level: debug|info|warn|error")
	logFile := flag.String("log-file", "", "optional benchmark runtime log file")
	trafficStatsInterval := flag.Duration("traffic-stats-interval", time.Second, "aggregated traffic stats log interval (e.g. 5s, 10s)")
	validateOrder := flag.Bool("validate-order", false, "enable strict in-order verification by sequence number (requires payload-size>=8)")
	pipelineProfile := flag.String("pipeline-profile", "empty", "pipeline profile for benchmark task: empty|basic|complex")
	flag.Parse()

	if *benchConfigPath != "" {
		// bench-config 作为“参数模板”，用于批量复现实验。
		// 覆盖策略是“配置文件优先于 flag 默认值”，但仍受后续校验约束。
		if err := applyBenchConfigFile(*benchConfigPath, mode, duration, warmup, payloadSize, workers, ppsPerWorker, ppsSweep, multicore, udpSinkReaders, udpSinkReadBuf, taskFastPath, taskPoolSize, taskQueueSize, taskChannelQueueSize, taskExecutionModel, receiverEventLoops, receiverReadBufferCap, tcpSenderConcurrency, basePort, logLevel, logFile, trafficStatsInterval, validateOrder, pipelineProfile); err != nil {
			logx.L().Errorw("load bench config failed", "error", err)
			os.Exit(2)
		}
	}

	if *payloadSize <= 0 || *payloadSize > 65535 {
		logx.L().Errorw("invalid payload-size", "payload_size", *payloadSize)
		os.Exit(2)
	}
	if *workers <= 0 {
		logx.L().Errorw("invalid workers", "workers", *workers)
		os.Exit(2)
	}
	if *validateOrder && *payloadSize < 8 {
		logx.L().Errorw("validate-order requires payload-size >= 8", "payload_size", *payloadSize)
		os.Exit(2)
	}

	if err := logx.Init(logx.Options{Level: *logLevel, File: *logFile, TrafficStatsSampleEvery: 1}); err != nil {
		logx.L().Errorw("log init failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = logx.Sync() }()
	logx.SetTrafficStatsInterval(*trafficStatsInterval)

	ctx := context.Background()
	rates := []int{*ppsPerWorker}
	if strings.TrimSpace(*ppsSweep) != "" {
		// sweep 模式用于“单次命令，多档位对比”，减少手工重复执行。
		parsed, err := parseSweep(*ppsSweep)
		if err != nil {
			logx.L().Errorw("invalid pps-sweep", "error", err, "pps_sweep", *ppsSweep)
			os.Exit(2)
		}
		rates = parsed
	}

	benchRun := func(proto string) {
		// 每个 proto 逐档位执行；任一档失败即终止，避免输出混合“成功/失败”数据误导对比。
		for _, rate := range rates {
			res, err := runForwardBenchmark(ctx, proto, *duration, *warmup, *payloadSize, *workers, rate, *multicore, *udpSinkReaders, *udpSinkReadBuf, *taskFastPath, *taskPoolSize, *taskQueueSize, *taskChannelQueueSize, *taskExecutionModel, *receiverEventLoops, *receiverReadBufferCap, *tcpSenderConcurrency, *basePort, *validateOrder, *pipelineProfile)
			if err != nil {
				logx.L().Errorw("benchmark failed", "proto", proto, "error", err)
				os.Exit(1)
			}
			printResult(res)
		}
	}

	switch *mode {
	case "udp", "tcp":
		benchRun(*mode)
	case "both":
		benchRun("udp")
		benchRun("tcp")
	default:
		logx.L().Errorw("invalid mode", "mode", *mode)
		os.Exit(2)
	}
}

// applyBenchConfigFile loads JSON config and overlays provided flag values.
//
// 维护提示：
//  - 新增 flag 时要同步补齐 benchFileConfig 字段和此处覆盖逻辑；
//  - 时长字段采用 ParseDuration，能统一支持 500ms/2s 等格式。
func applyBenchConfigFile(path string, mode *string, duration, warmup *time.Duration, payloadSize, workers, ppsPerWorker *int, ppsSweep *string, multicore *bool, udpSinkReaders, udpSinkReadBuf *int, taskFastPath *bool, taskPoolSize, taskQueueSize, taskChannelQueueSize *int, taskExecutionModel *string, receiverEventLoops, receiverReadBufferCap, tcpSenderConcurrency, basePort *int, logLevel, logFile *string, trafficStatsInterval *time.Duration, validateOrder *bool, pipelineProfile *string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg benchFileConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return err
	}
	if cfg.Mode != "" {
		*mode = cfg.Mode
	}
	if cfg.Duration != "" {
		d, err := time.ParseDuration(cfg.Duration)
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}
		*duration = d
	}
	if cfg.Warmup != "" {
		d, err := time.ParseDuration(cfg.Warmup)
		if err != nil {
			return fmt.Errorf("invalid warmup: %w", err)
		}
		*warmup = d
	}
	if cfg.PayloadSize != nil {
		*payloadSize = *cfg.PayloadSize
	}
	if cfg.Workers != nil {
		*workers = *cfg.Workers
	}
	if cfg.PPSPerWorker != nil {
		*ppsPerWorker = *cfg.PPSPerWorker
	}
	if cfg.PPSSweep != "" {
		*ppsSweep = cfg.PPSSweep
	}
	if cfg.Multicore != nil {
		*multicore = *cfg.Multicore
	}
	if cfg.UDPSinkReaders != nil {
		*udpSinkReaders = *cfg.UDPSinkReaders
	}
	if cfg.UDPSinkReadBuf != nil {
		*udpSinkReadBuf = *cfg.UDPSinkReadBuf
	}
	if cfg.TaskFastPath != nil {
		*taskFastPath = *cfg.TaskFastPath
	}
	if cfg.TaskPoolSize != nil {
		*taskPoolSize = *cfg.TaskPoolSize
	}
	if cfg.TaskQueueSize != nil {
		*taskQueueSize = *cfg.TaskQueueSize
	}
	if cfg.TaskChannelQueueSize != nil {
		*taskChannelQueueSize = *cfg.TaskChannelQueueSize
	}
	if cfg.TaskExecutionModel != "" {
		*taskExecutionModel = cfg.TaskExecutionModel
	}

	if cfg.ReceiverEventLoops != nil {
		*receiverEventLoops = *cfg.ReceiverEventLoops
	}
	if cfg.ReceiverReadBuffer != nil {
		*receiverReadBufferCap = *cfg.ReceiverReadBuffer
	}
	if cfg.TCPSenderConcurrency != nil {
		*tcpSenderConcurrency = *cfg.TCPSenderConcurrency
	}
	if cfg.BasePort != nil {
		*basePort = *cfg.BasePort
	}
	if cfg.LogLevel != "" {
		*logLevel = cfg.LogLevel
	}
	if cfg.LogFile != "" {
		*logFile = cfg.LogFile
	}
	if cfg.TrafficStatsInterval != "" {
		d, err := time.ParseDuration(cfg.TrafficStatsInterval)
		if err != nil {
			return fmt.Errorf("invalid traffic_stats_interval: %w", err)
		}
		*trafficStatsInterval = d
	}
	if cfg.ValidateOrder != nil {
		*validateOrder = *cfg.ValidateOrder
	}
	if cfg.PipelineProfile != "" {
		*pipelineProfile = cfg.PipelineProfile
	}
	return nil
}

// parseSweep parses comma-separated non-negative pps values.
//
// 例如 "1000,2000,4000" -> [1000,2000,4000]。
func parseSweep(in string) ([]int, error) {
	parts := strings.Split(in, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return nil, fmt.Errorf("invalid value %q", p)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty sweep")
	}
	return out, nil
}

// runForwardBenchmark runs one complete benchmark round for a given proto.
//
// 该函数是 bench 的核心执行器，包含：
//  1. 启动本地 sink；
//  2. 构建最小 runtime 拓扑；
//  3. 启动 generator 并执行 warmup + measure；
//  4. 汇总 sent/recv/order/loss/pps/mbps；
//  5. 关闭生成器、runtime 与 sink。
//
// 维护提示：
//  - warmup 基线必须在测量前采样，否则结果会被启动抖动污染；
//  - sink drain 等待用于降低“发送已停但接收还在入队”的尾部偏差。
func runForwardBenchmark(ctx context.Context, proto string, duration, warmup time.Duration, payloadSize, workers, ppsPerWorker int, multicore bool, udpSinkReaders, udpSinkReadBuf int, taskFastPath bool, taskPoolSize, taskQueueSize, taskChannelQueueSize int, taskExecutionModel string, receiverEventLoops, receiverReadBufferCap, tcpSenderConcurrency, basePort int, validateOrder bool, pipelineProfile string) (*result, error) {
	m := &metrics{seqCheck: validateOrder}
	if basePort == 0 {
		basePort = map[string]int{"udp": 19100, "tcp": 19200}[proto]
	}
	if basePort == 0 {
		return nil, fmt.Errorf("unknown proto %s", proto)
	}

	var (
		sinkAddr string
		stopSink func() error
		err      error
	)

	switch proto {
	case "udp":
		sinkAddr, stopSink, err = startUDPSink(basePort+1, m, udpSinkReaders, udpSinkReadBuf)
	case "tcp":
		sinkAddr, stopSink, err = startTCPSink(basePort+1, m)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = stopSink() }()

	rt := app.NewRuntime()
	// benchConfig 构造单 receiver/single task/single sender 的最小拓扑，
	// 让不同场景之间的差异主要来自执行模型与参数，而不是业务编排差异。
	cfg := benchConfig(proto, basePort, sinkAddr, multicore, taskFastPath, taskPoolSize, taskQueueSize, taskChannelQueueSize, taskExecutionModel, receiverEventLoops, receiverReadBufferCap, tcpSenderConcurrency, workers, pipelineProfile)
	if err := rt.UpdateCache(ctx, cfg); err != nil {
		return nil, fmt.Errorf("update cache: %w", err)
	}
	defer func() { _ = rt.Stop(ctx) }()
	time.Sleep(200 * time.Millisecond)

	stopGen := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			switch proto {
			case "udp":
				udpGenerate(basePort, payloadSize, ppsPerWorker, m, stopGen, validateOrder)
			case "tcp":
				tcpGenerate(basePort, payloadSize, ppsPerWorker, m, stopGen, validateOrder)
			}
		}(i)
	}

	// warmup 期间的数据不计入最终结果，用于避开启动瞬态。
	time.Sleep(warmup)
	baseSentPackets := atomic.LoadUint64(&m.sentPackets)
	baseSentBytes := atomic.LoadUint64(&m.sentBytes)
	baseRecvPackets := atomic.LoadUint64(&m.recvPackets)
	baseRecvBytes := atomic.LoadUint64(&m.recvBytes)
	baseOrderErrors := atomic.LoadUint64(&m.orderErrors)

	// measurement window：在固定时长内采样增量。
	start := time.Now()
	time.Sleep(duration)
	elapsed := time.Since(start)
	close(stopGen)
	wg.Wait()
	waitForSinkDrain(m, 2*time.Second, 150*time.Millisecond)

	totalSentPackets := atomic.LoadUint64(&m.sentPackets)
	totalSentBytes := atomic.LoadUint64(&m.sentBytes)
	totalRecvPackets := atomic.LoadUint64(&m.recvPackets)
	totalRecvBytes := atomic.LoadUint64(&m.recvBytes)
	totalOrderErrors := atomic.LoadUint64(&m.orderErrors)

	sentPackets := totalSentPackets - baseSentPackets
	sentBytes := totalSentBytes - baseSentBytes
	recvPackets := totalRecvPackets - baseRecvPackets
	recvBytes := totalRecvBytes - baseRecvBytes
	orderErrors := totalOrderErrors - baseOrderErrors

	loss := 0.0
	if sentPackets > 0 {
		if recvPackets >= sentPackets {
			loss = 0
		} else {
			loss = float64(sentPackets-recvPackets) / float64(sentPackets)
		}
	}

	return &result{
		proto:          proto,
		duration:       elapsed,
		payloadSize:    payloadSize,
		senderWorkers:  workers,
		sentPackets:    sentPackets,
		sentBytes:      sentBytes,
		recvPackets:    recvPackets,
		recvBytes:      recvBytes,
		packetLossRate: loss,
		orderErrors:    orderErrors,
		strictOrderOK:  !validateOrder || orderErrors == 0,
		pps:            float64(recvPackets) / elapsed.Seconds(),
		mbps:           float64(recvBytes*8) / elapsed.Seconds() / 1_000_000,
	}, nil
}

func waitForSinkDrain(m *metrics, maxWait, stableFor time.Duration) {
	if m == nil || maxWait <= 0 {
		return
	}
	deadline := time.Now().Add(maxWait)
	last := atomic.LoadUint64(&m.recvPackets)
	stableSince := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
		cur := atomic.LoadUint64(&m.recvPackets)
		if cur != last {
			last = cur
			stableSince = time.Now()
			continue
		}
		if time.Since(stableSince) >= stableFor {
			return
		}
	}
}

// benchConfig builds runtime config used by benchmark scenarios.
//
// 设计意图：
//  - 用最少配置元素保证链路真实可跑；
//  - 将调参重点收敛到执行模型、并发和缓冲参数。
func benchConfig(proto string, basePort int, sinkAddr string, multicore, taskFastPath bool, taskPoolSize, taskQueueSize, taskChannelQueueSize int, taskExecutionModel string, receiverEventLoops, receiverReadBufferCap, tcpSenderConcurrency, workers int, pipelineProfile string) config.Config {
	rc := config.ReceiverConfig{Multicore: &multicore, NumEventLoop: receiverEventLoops, ReadBufferCap: receiverReadBufferCap}
	sc := config.SenderConfig{Concurrency: 1}
	switch proto {
	case "udp":
		rc.Type = "udp_gnet"
		rc.Listen = fmt.Sprintf("udp://127.0.0.1:%d", basePort)
		sc.Type = "udp_unicast"
		sc.LocalIP = "127.0.0.1"
		sc.LocalPort = basePort + 2
		sc.Remote = sinkAddr
		if workers > 1 {
			sc.Concurrency = workers
		}
	case "tcp":
		rc.Type = "tcp_gnet"
		rc.Listen = fmt.Sprintf("tcp://127.0.0.1:%d", basePort)
		rc.Frame = "u16be"
		sc.Type = "tcp_gnet"
		sc.Remote = sinkAddr
		sc.Frame = "u16be"
		sc.Concurrency = tcpSenderConcurrency
	}
	stages := []config.StageConfig{}
	switch strings.ToLower(strings.TrimSpace(pipelineProfile)) {
	case "", "empty":
		stages = []config.StageConfig{}
	case "basic":
		stages = []config.StageConfig{{Type: "replace_offset_bytes", Offset: 0, Hex: "aabbccdd"}}
	case "complex":
		flag := true
		stages = []config.StageConfig{
			{Type: "replace_offset_bytes", Offset: 0, Hex: "aabbccdd"},
			{Type: "mark_as_file_chunk", Path: "/bench/out.bin", Bool: &flag},
			{Type: "clear_file_meta"},
		}
	default:
		stages = []config.StageConfig{}
	}

	return config.Config{
		Version: 1,
		Logging: config.LoggingConfig{
			Level: "warn",
		},
		Receivers: map[string]config.ReceiverConfig{
			"in": rc,
		},
		Senders: map[string]config.SenderConfig{
			"out": sc,
		},
		Pipelines: map[string][]config.StageConfig{
			"p": stages,
		},
		Tasks: map[string]config.TaskConfig{
			"t": {
				PoolSize:         taskPoolSize,
				QueueSize:        taskQueueSize,
				FastPath:         taskFastPath,
				ExecutionModel:   taskExecutionModel,
				ChannelQueueSize: taskChannelQueueSize,
				Receivers:        []string{"in"},
				Pipelines:        []string{"p"},
				Senders:          []string{"out"},
			},
		},
	}
}

// startUDPSink starts local UDP sink readers and accumulates receive metrics.
//
// readers 参数用于模拟“接收端并发消费能力”，避免 sink 自身成为单点瓶颈。
func startUDPSink(port int, m *metrics, readers, readBuf int) (string, func() error, error) {
	pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", nil, err
	}
	if uc, ok := pc.(*net.UDPConn); ok && readBuf > 0 {
		_ = uc.SetReadBuffer(readBuf)
	}
	stop := make(chan struct{})
	if readers <= 0 {
		readers = 1
	}
	for i := 0; i < readers; i++ {
		go func() {
			buf := make([]byte, 65535)
			for {
				_ = pc.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
				n, _, err := pc.ReadFrom(buf)
				if err != nil {
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						select {
						case <-stop:
							return
						default:
							continue
						}
					}
					return
				}
				if m.seqCheck && !validateSequence(buf[:n], m) {
					atomic.AddUint64(&m.orderErrors, 1)
				}
				atomic.AddUint64(&m.recvPackets, 1)
				atomic.AddUint64(&m.recvBytes, uint64(n))
			}
		}()
	}
	return fmt.Sprintf("127.0.0.1:%d", port), func() error {
		close(stop)
		return pc.Close()
	}, nil
}

// startTCPSink starts a local TCP sink and reads u16-be framed payloads.
//
// TCP sink 和 sender 默认都使用 u16be framing，确保测试链路边界一致。
func startTCPSink(port int, m *metrics) (string, func() error, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", nil, err
	}
	stop := make(chan struct{})
	go func() {
		for {
			tcpLn := ln.(*net.TCPListener)
			_ = tcpLn.SetDeadline(time.Now().Add(200 * time.Millisecond))
			conn, err := ln.Accept()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					select {
					case <-stop:
						return
					default:
						continue
					}
				}
				return
			}
			go readU16Framed(conn, m)
		}
	}()
	return fmt.Sprintf("127.0.0.1:%d", port), func() error {
		close(stop)
		return ln.Close()
	}, nil
}

// readU16Framed reads length-prefixed payload stream and updates metrics.
func readU16Framed(conn net.Conn, m *metrics) {
	defer conn.Close()
	hdr := make([]byte, 2)
	buf := make([]byte, 0, 4096)
	for {
		if _, err := io.ReadFull(conn, hdr); err != nil {
			return
		}
		n := int(binary.BigEndian.Uint16(hdr))
		if cap(buf) < n {
			buf = make([]byte, n)
		} else {
			buf = buf[:n]
		}
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		if m.seqCheck && !validateSequence(buf, m) {
			atomic.AddUint64(&m.orderErrors, 1)
		}
		atomic.AddUint64(&m.recvPackets, 1)
		atomic.AddUint64(&m.recvBytes, uint64(n))
	}
}

// udpGenerate continuously sends fixed-size UDP payloads to benchmark receiver.
//
// withSeq=true 时会把自增序号写入前 8 字节，用于严格顺序校验。
func udpGenerate(port, payloadSize, ppsPerWorker int, m *metrics, stop <-chan struct{}, withSeq bool) {
	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return
	}
	defer conn.Close()
	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i)
	}
	limiter := newPPSLimiter(ppsPerWorker)
	for {
		limiter.Wait()
		select {
		case <-stop:
			return
		default:
		}
		if withSeq {
			seq := atomic.AddUint64(&m.seqAlloc, 1) - 1
			binary.BigEndian.PutUint64(payload[:8], seq)
		}
		n, err := conn.Write(payload)
		if err != nil {
			time.Sleep(1 * time.Millisecond)
			continue
		}
		atomic.AddUint64(&m.sentPackets, 1)
		atomic.AddUint64(&m.sentBytes, uint64(n))
	}
}

// tcpGenerate continuously sends u16-framed TCP payloads to benchmark receiver.
//
// 当连接写失败时会自动重连，避免短暂连接抖动直接终止整个 worker。
func tcpGenerate(port, payloadSize, ppsPerWorker int, m *metrics, stop <-chan struct{}, withSeq bool) {
	dial := func() net.Conn {
		for {
			conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err == nil {
				return conn
			}
			select {
			case <-stop:
				return nil
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	conn := dial()
	if conn == nil {
		return
	}
	defer conn.Close()

	frame := make([]byte, 2+payloadSize)
	binary.BigEndian.PutUint16(frame[:2], uint16(payloadSize))
	for i := range frame[2:] {
		frame[2+i] = byte(i)
	}
	limiter := newPPSLimiter(ppsPerWorker)
	for {
		limiter.Wait()
		select {
		case <-stop:
			return
		default:
		}
		if withSeq {
			seq := atomic.AddUint64(&m.seqAlloc, 1) - 1
			binary.BigEndian.PutUint64(frame[2:10], seq)
		}
		n, err := conn.Write(frame)
		if err != nil {
			_ = conn.Close()
			conn = dial()
			if conn == nil {
				return
			}
			continue
		}
		atomic.AddUint64(&m.sentPackets, 1)
		atomic.AddUint64(&m.sentBytes, uint64(n-2))
	}
}

type ppsLimiter struct {
	pps         int
	tokens      float64
	lastRefill  time.Time
	refillEvery time.Duration
}

// newPPSLimiter creates token-bucket like limiter.
// pps<=0 means unlimited send rate.
func newPPSLimiter(pps int) *ppsLimiter {
	return &ppsLimiter{pps: pps, lastRefill: time.Now(), refillEvery: 2 * time.Millisecond}
}

// Wait blocks until one send token is available.
// 该实现强调“简单稳定”而非纳秒级精度，适合 benchmark 速率分档。
func (l *ppsLimiter) Wait() {
	if l == nil || l.pps <= 0 {
		return
	}
	for {
		now := time.Now()
		elapsed := now.Sub(l.lastRefill)
		if elapsed > 0 {
			l.tokens += elapsed.Seconds() * float64(l.pps)
			if l.tokens > float64(l.pps) {
				l.tokens = float64(l.pps)
			}
			l.lastRefill = now
		}
		if l.tokens >= 1 {
			l.tokens--
			return
		}
		time.Sleep(l.refillEvery)
	}
}

// printResult logs one benchmark round in structured form.
//
// 日志字段设计为可直接用于后处理（例如 jq/脚本筛选 top 场景）。
func printResult(r *result) {
	if r == nil {
		return
	}
	logx.L().Infow("forward benchmark result",
		"proto", r.proto,
		"duration", r.duration.Round(time.Millisecond).String(),
		"payload_size", r.payloadSize,
		"workers", r.senderWorkers,
		"sent_packets", r.sentPackets,
		"sent_bytes", r.sentBytes,
		"recv_packets", r.recvPackets,
		"recv_bytes", r.recvBytes,
		"loss_rate", r.packetLossRate,
		"order_errors", r.orderErrors,
		"strict_order_ok", r.strictOrderOK,
		"pps", r.pps,
		"mbps", r.mbps,
	)
}

func validateSequence(payload []byte, m *metrics) bool {
	if m == nil || !m.seqCheck {
		return true
	}
	if len(payload) < 8 {
		return false
	}
	seq := binary.BigEndian.Uint64(payload[:8])
	for {
		if atomic.LoadUint32(&m.seqInit) == 0 {
			if atomic.CompareAndSwapUint32(&m.seqInit, 0, 1) {
				atomic.StoreUint64(&m.expectedSeq, seq+1)
				return true
			}
			continue
		}
		expected := atomic.LoadUint64(&m.expectedSeq)
		if seq != expected {
			return false
		}
		if atomic.CompareAndSwapUint64(&m.expectedSeq, expected, expected+1) {
			return true
		}
	}
}

// max returns larger value between a and b.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
