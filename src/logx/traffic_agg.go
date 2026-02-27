// traffic_agg.go 提供流量统计聚合器并定期输出指标日志。
package logx

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"forward-stub/src/packet"
)

var trafficStatsIntervalNanos atomic.Int64
var trafficStatsSampleEvery atomic.Int64

// init 负责该函数对应的核心逻辑，详见实现细节。
func init() {
	trafficStatsIntervalNanos.Store(int64(time.Second))
	trafficStatsSampleEvery.Store(1)
}

// SetTrafficStatsInterval 负责该函数对应的核心逻辑，详见实现细节。
func SetTrafficStatsInterval(d time.Duration) {
	if d <= 0 {
		d = time.Second
	}
	trafficStatsIntervalNanos.Store(int64(d))
}

// SetTrafficStatsSampleEvery 设置流量统计采样倍率（N=1 表示不采样）。
func SetTrafficStatsSampleEvery(n int) {
	if n <= 0 {
		n = 1
	}
	trafficStatsSampleEvery.Store(int64(n))
}

func trafficStatsSampleN() uint64 {
	v := trafficStatsSampleEvery.Load()
	if v <= 0 {
		return 1
	}
	return uint64(v)
}

// trafficStatsInterval 负责该函数对应的核心逻辑，详见实现细节。
func trafficStatsInterval() time.Duration {
	n := trafficStatsIntervalNanos.Load()
	if n <= 0 {
		return time.Second
	}
	return time.Duration(n)
}

type TaskRuntimeStats struct {
	PoolSize    int
	PoolRunning int
	PoolFree    int
	PoolWaiting int
	Inflight    int64
	FastPath    bool
}

var taskRuntimeStatsMu sync.RWMutex
var taskRuntimeStatsFn = make(map[string]func() TaskRuntimeStats)

// RegisterTaskRuntimeStats 注册 task 运行时统计回调，供聚合日志输出线程池状态。
func RegisterTaskRuntimeStats(task string, fn func() TaskRuntimeStats) {
	if task == "" || fn == nil {
		return
	}
	taskRuntimeStatsMu.Lock()
	taskRuntimeStatsFn[task] = fn
	taskRuntimeStatsMu.Unlock()
}

// UnregisterTaskRuntimeStats 注销 task 运行时统计回调。
func UnregisterTaskRuntimeStats(task string) {
	if task == "" {
		return
	}
	taskRuntimeStatsMu.Lock()
	delete(taskRuntimeStatsFn, task)
	taskRuntimeStatsMu.Unlock()
}

func lookupTaskRuntimeStats(task string) (TaskRuntimeStats, bool) {
	taskRuntimeStatsMu.RLock()
	fn := taskRuntimeStatsFn[task]
	taskRuntimeStatsMu.RUnlock()
	if fn == nil {
		return TaskRuntimeStats{}, false
	}
	return fn(), true
}

type trafficCounter struct {
	msg    string
	fields []any
	role   string
	name   string
	key    string
	sample uint64

	packets atomic.Uint64
	bytes   atomic.Uint64
	lastPkt atomic.Uint64
	lastB   atomic.Uint64
	refs    atomic.Int64
	seen    atomic.Uint64
}

type TrafficCounter struct {
	hub *trafficStatsHub
	key string
	c   *trafficCounter
}

// AddBytes 负责该函数对应的核心逻辑，详见实现细节。
func (tc *TrafficCounter) AddBytes(n int) {
	if tc == nil || tc.c == nil || n <= 0 {
		return
	}
	seq := tc.c.seen.Add(1)
	if seq%tc.c.sample != 0 {
		return
	}
	tc.c.packets.Add(tc.c.sample)
	tc.c.bytes.Add(uint64(n) * tc.c.sample)
}

// Close 负责该函数对应的核心逻辑，详见实现细节。
func (tc *TrafficCounter) Close() {
	if tc == nil || tc.hub == nil || tc.key == "" {
		return
	}
	tc.hub.release(tc.key)
	tc.hub = nil
	tc.c = nil
	tc.key = ""
}

type trafficStatsHub struct {
	mu       sync.RWMutex
	counters map[string]*trafficCounter

	startOnce sync.Once
	stopCh    chan struct{}
}

var globalTrafficHub = &trafficStatsHub{
	counters: make(map[string]*trafficCounter),
	stopCh:   make(chan struct{}),
}

// AcquireTrafficCounter 负责该函数对应的核心逻辑，详见实现细节。
func AcquireTrafficCounter(msg string, fields ...any) *TrafficCounter {
	return globalTrafficHub.acquire(msg, fields...)
}

// acquire 负责该函数对应的核心逻辑，详见实现细节。
func (h *trafficStatsHub) acquire(msg string, fields ...any) *TrafficCounter {
	h.startOnce.Do(func() { go h.loop() })
	key := buildTrafficKey(msg, fields)

	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.counters[key]; ok {
		c.refs.Add(1)
		return &TrafficCounter{hub: h, key: key, c: c}
	}
	c := &trafficCounter{msg: msg, fields: append([]any(nil), fields...)}
	c.role, c.name, c.key = parseTrafficMeta(fields)
	c.sample = trafficStatsSampleN()
	c.refs.Store(1)
	h.counters[key] = c
	return &TrafficCounter{hub: h, key: key, c: c}
}

// release 负责该函数对应的核心逻辑，详见实现细节。
func (h *trafficStatsHub) release(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.counters[key]
	if !ok {
		return
	}
	if c.refs.Add(-1) <= 0 {
		delete(h.counters, key)
	}
}

// loop 负责该函数对应的核心逻辑，详见实现细节。
func (h *trafficStatsHub) loop() {
	startedAt := time.Now()
	interval := trafficStatsInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.flush(time.Since(startedAt), interval)
			nextInterval := trafficStatsInterval()
			if nextInterval != interval {
				ticker.Stop()
				interval = nextInterval
				ticker = time.NewTicker(interval)
			}
		case <-h.stopCh:
			return
		}
	}
}

// flush 负责该函数对应的核心逻辑，详见实现细节。
func (h *trafficStatsHub) flush(elapsed, interval time.Duration) {
	h.mu.RLock()
	cs := make([]*trafficCounter, 0, len(h.counters))
	for _, c := range h.counters {
		cs = append(cs, c)
	}
	h.mu.RUnlock()
	if len(cs) == 0 {
		return
	}
	summary := newTrafficSummary(elapsed)
	for _, c := range cs {
		pkts := c.packets.Load()
		b := c.bytes.Load()
		if pkts == 0 {
			continue
		}
		summary.add(c, pkts, b, interval)
	}
	summary.setMemoryPool()
	if summary.hasData() {
		summary.log()
	}
}

type trafficSummary struct {
	uptime     string
	task       []string
	memoryPool string
}

func newTrafficSummary(elapsed time.Duration) *trafficSummary {
	return &trafficSummary{uptime: elapsed.Truncate(time.Millisecond).String()}
}

func (s *trafficSummary) add(c *trafficCounter, packets, bytes uint64, interval time.Duration) {
	line := fmt.Sprintf("task=%s dir=%s total_packets=%d total_bytes=%d", c.name, c.key, packets, bytes)
	if c.name == "" {
		line = fmt.Sprintf("%s total_packets=%d total_bytes=%d", c.msg, packets, bytes)
	}

	if interval > 0 {
		lastPackets := c.lastPkt.Swap(packets)
		lastBytes := c.lastB.Swap(bytes)
		intervalPackets := diffCounter(packets, lastPackets)
		intervalBytes := diffCounter(bytes, lastBytes)
		seconds := interval.Seconds()
		if seconds > 0 {
			line = fmt.Sprintf(
				"%s | interval_packets=%d interval_bytes=%d pps=%.2f bps=%.2f",
				line,
				intervalPackets,
				intervalBytes,
				float64(intervalPackets)/seconds,
				float64(intervalBytes)/seconds,
			)
		}
	}

	switch c.role {
	case "task":
		if runtime, ok := lookupTaskRuntimeStats(c.name); ok {
			line = fmt.Sprintf(
				"%s | worker_pool={size=%d running=%d free=%d waiting=%d inflight=%d fast_path=%t}",
				line,
				runtime.PoolSize,
				runtime.PoolRunning,
				runtime.PoolFree,
				runtime.PoolWaiting,
				runtime.Inflight,
				runtime.FastPath,
			)
		}
		s.task = append(s.task, line)
	}
}

func diffCounter(cur, prev uint64) uint64 {
	if cur >= prev {
		return cur - prev
	}
	return cur
}

func (s *trafficSummary) setMemoryPool() {
	pool := packet.SnapshotPayloadPoolStats()
	s.memoryPool = fmt.Sprintf(
		"inuse_buffers=%d inuse_bytes=%d cached_buffers=%d cached_bytes=%d total_buffers=%d total_bytes=%d gets=%d puts=%d",
		pool.InUseBuffers,
		pool.InUseBytes,
		pool.CachedBuffers,
		pool.CachedBytes,
		pool.TotalBuffers,
		pool.TotalBytes,
		pool.Gets,
		pool.Puts,
	)
}

func (s *trafficSummary) hasData() bool {
	return len(s.task) > 0
}

func (s *trafficSummary) log() {
	tasks := strings.Join(s.task, "\n")
	L().Infow("traffic stats summary", "uptime", s.uptime, "memory_pool", s.memoryPool, "tasks", tasks)
}

func parseTrafficMeta(fields []any) (role string, name string, key string) {
	for i := 0; i < len(fields)-1; i += 2 {
		k, ok := fields[i].(string)
		if !ok {
			continue
		}
		v := fmt.Sprint(fields[i+1])
		switch k {
		case "role":
			role = v
		case "receiver", "task", "sender":
			name = v
		case "receiver_key", "sender_key", "direction":
			key = v
		}
	}
	return role, name, key
}

// buildTrafficKey 负责该函数对应的核心逻辑，详见实现细节。
func buildTrafficKey(msg string, fields []any) string {
	var sb strings.Builder
	sb.WriteString(msg)
	for i := 0; i < len(fields)-1; i += 2 {
		sb.WriteString("|")
		sb.WriteString(fmt.Sprint(fields[i]))
		sb.WriteString("=")
		sb.WriteString(fmt.Sprint(fields[i+1]))
	}
	return sb.String()
}
