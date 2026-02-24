package main

import (
	"context"
	"encoding/binary"
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

	"forword-stub/src/app"
	"forword-stub/src/config"
	"forword-stub/src/logx"
)

type metrics struct {
	sentPackets uint64
	sentBytes   uint64
	recvPackets uint64
	recvBytes   uint64
}

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
	pps            float64
	mbps           float64
}

func main() {
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
	taskPoolSize := flag.Int("task-pool-size", 2048, "benchmark task worker pool size when fast_path=false")
	logLevel := flag.String("log-level", "warn", "benchmark runtime log level: debug|info|warn|error")
	logFile := flag.String("log-file", "", "optional benchmark runtime log file")
	trafficStatsInterval := flag.Duration("traffic-stats-interval", time.Second, "aggregated traffic stats log interval (e.g. 5s, 10s)")
	flag.Parse()

	if *payloadSize <= 0 || *payloadSize > 65535 {
		fmt.Fprintf(os.Stderr, "invalid payload-size: %d\n", *payloadSize)
		os.Exit(2)
	}
	if *workers <= 0 {
		fmt.Fprintf(os.Stderr, "invalid workers: %d\n", *workers)
		os.Exit(2)
	}

	if err := logx.Init(logx.Options{Level: *logLevel, File: *logFile}); err != nil {
		fmt.Fprintf(os.Stderr, "log init failed: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logx.Sync() }()
	logx.SetTrafficStatsInterval(*trafficStatsInterval)

	ctx := context.Background()
	rates := []int{*ppsPerWorker}
	if strings.TrimSpace(*ppsSweep) != "" {
		parsed, err := parseSweep(*ppsSweep)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid pps-sweep: %v\n", err)
			os.Exit(2)
		}
		rates = parsed
	}

	benchRun := func(proto string) {
		for _, rate := range rates {
			res, err := runForwardBenchmark(ctx, proto, *duration, *warmup, *payloadSize, *workers, rate, *multicore, *udpSinkReaders, *udpSinkReadBuf, *taskFastPath, *taskPoolSize)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%s] benchmark failed: %v\n", proto, err)
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
		fmt.Fprintf(os.Stderr, "invalid mode: %s\n", *mode)
		os.Exit(2)
	}
}

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

func runForwardBenchmark(ctx context.Context, proto string, duration, warmup time.Duration, payloadSize, workers, ppsPerWorker int, multicore bool, udpSinkReaders, udpSinkReadBuf int, taskFastPath bool, taskPoolSize int) (*result, error) {
	m := &metrics{}
	basePort := map[string]int{"udp": 19100, "tcp": 19200}[proto]
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
	cfg := benchConfig(proto, basePort, sinkAddr, multicore, taskFastPath, taskPoolSize)
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
				udpGenerate(basePort, payloadSize, ppsPerWorker, m, stopGen)
			case "tcp":
				tcpGenerate(basePort, payloadSize, ppsPerWorker, m, stopGen)
			}
		}(i)
	}

	time.Sleep(warmup)
	atomic.StoreUint64(&m.sentPackets, 0)
	atomic.StoreUint64(&m.sentBytes, 0)
	atomic.StoreUint64(&m.recvPackets, 0)
	atomic.StoreUint64(&m.recvBytes, 0)

	start := time.Now()
	time.Sleep(duration)
	elapsed := time.Since(start)
	close(stopGen)
	wg.Wait()
	time.Sleep(300 * time.Millisecond)

	sentPackets := atomic.LoadUint64(&m.sentPackets)
	sentBytes := atomic.LoadUint64(&m.sentBytes)
	recvPackets := atomic.LoadUint64(&m.recvPackets)
	recvBytes := atomic.LoadUint64(&m.recvBytes)

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
		pps:            float64(recvPackets) / elapsed.Seconds(),
		mbps:           float64(recvBytes*8) / elapsed.Seconds() / 1_000_000,
	}, nil
}

func benchConfig(proto string, basePort int, sinkAddr string, multicore, taskFastPath bool, taskPoolSize int) config.Config {
	rc := config.ReceiverConfig{Multicore: multicore}
	sc := config.SenderConfig{Concurrency: 1}
	switch proto {
	case "udp":
		rc.Type = "udp_gnet"
		rc.Listen = fmt.Sprintf("udp://127.0.0.1:%d", basePort)
		sc.Type = "udp_unicast"
		sc.LocalIP = "127.0.0.1"
		sc.LocalPort = basePort + 2
		sc.Remote = sinkAddr
	case "tcp":
		rc.Type = "tcp_gnet"
		rc.Listen = fmt.Sprintf("tcp://127.0.0.1:%d", basePort)
		rc.Frame = "u16be"
		sc.Type = "tcp_gnet"
		sc.Remote = sinkAddr
		sc.Frame = "u16be"
		sc.Concurrency = 4
	}
	return config.Config{
		Version: 1,
		Logging: config.LoggingConfig{Level: "warn"},
		Receivers: map[string]config.ReceiverConfig{
			"in": rc,
		},
		Senders: map[string]config.SenderConfig{
			"out": sc,
		},
		Pipelines: map[string][]config.StageConfig{
			"p": {},
		},
		Tasks: map[string]config.TaskConfig{
			"t": {
				PoolSize:  taskPoolSize,
				FastPath:  taskFastPath,
				Receivers: []string{"in"},
				Pipelines: []string{"p"},
				Senders:   []string{"out"},
			},
		},
	}
}

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

func readU16Framed(conn net.Conn, m *metrics) {
	defer conn.Close()
	hdr := make([]byte, 2)
	for {
		if _, err := io.ReadFull(conn, hdr); err != nil {
			return
		}
		n := int(binary.BigEndian.Uint16(hdr))
		buf := make([]byte, n)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		atomic.AddUint64(&m.recvPackets, 1)
		atomic.AddUint64(&m.recvBytes, uint64(n))
	}
}

func udpGenerate(port, payloadSize, ppsPerWorker int, m *metrics, stop <-chan struct{}) {
	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return
	}
	defer conn.Close()
	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i)
	}

	for {
		throttle(ppsPerWorker)
		select {
		case <-stop:
			return
		default:
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

func tcpGenerate(port, payloadSize, ppsPerWorker int, m *metrics, stop <-chan struct{}) {
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

	for {
		throttle(ppsPerWorker)
		select {
		case <-stop:
			return
		default:
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

func throttle(pps int) {
	if pps <= 0 {
		return
	}
	time.Sleep(time.Second / time.Duration(pps))
}

func printResult(r *result) {
	fmt.Printf("\n=== %s forward benchmark ===\n", r.proto)
	fmt.Printf("duration=%s payload=%dB workers=%d\n", r.duration.Round(time.Millisecond), r.payloadSize, r.senderWorkers)
	fmt.Printf("sent: packets=%d bytes=%d\n", r.sentPackets, r.sentBytes)
	fmt.Printf("recv: packets=%d bytes=%d\n", r.recvPackets, r.recvBytes)
	fmt.Printf("loss: %.2f%%\n", r.packetLossRate*100)
	fmt.Printf("throughput: %.0f pps, %.2f Mbps\n", r.pps, r.mbps)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
