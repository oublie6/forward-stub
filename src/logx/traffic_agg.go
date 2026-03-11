// traffic_agg.go 提供流量统计聚合器并定期输出指标日志。
package logx

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
	PoolSize       int
	PoolRunning    int
	PoolFree       int
	PoolWaiting    int
	QueueSize      int
	QueueAvailable int
	Inflight       int64
	FastPath       bool
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

func listTaskRuntimeStats() map[string]TaskRuntimeStats {
	taskRuntimeStatsMu.RLock()
	defer taskRuntimeStatsMu.RUnlock()
	out := make(map[string]TaskRuntimeStats, len(taskRuntimeStatsFn))
	for task, fn := range taskRuntimeStatsFn {
		if fn == nil {
			continue
		}
		out[task] = fn()
	}
	return out
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
	// closed 仅用于避免 Close/AddBytes 并发时的重复释放与数据竞争。
	closed atomic.Bool
}

// AddBytes 负责该函数对应的核心逻辑，详见实现细节。
func (tc *TrafficCounter) AddBytes(n int) {
	if tc == nil || tc.c == nil || n <= 0 {
		return
	}
	if tc.closed.Load() {
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
	if !tc.closed.CompareAndSwap(false, true) {
		return
	}
	tc.hub.release(tc.key)
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
	h.flush(time.Since(startedAt), interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.flush(time.Since(startedAt), interval)
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
	summary := newTrafficSummary(elapsed)
	seenTask := make(map[string]struct{})
	for _, c := range cs {
		pkts := c.packets.Load()
		b := c.bytes.Load()
		if pkts == 0 && c.role != "task" {
			continue
		}
		summary.add(c, pkts, b, interval)
		if c.role == "task" && c.name != "" {
			seenTask[c.name] = struct{}{}
		}
	}
	for task, runtime := range listTaskRuntimeStats() {
		if _, ok := seenTask[task]; ok {
			continue
		}
		summary.addRuntimeOnlyTask(task, runtime)
	}
	if summary.hasData() {
		summary.log()
	}
}

type trafficSummary struct {
	Uptime string               `json:"uptime"`
	Tasks  []taskAggregateStats `json:"tasks"`
}

type taskAggregateStats struct {
	Task            string           `json:"task"`
	TotalPackets    uint64           `json:"total_packets"`
	TotalBytes      uint64           `json:"total_bytes"`
	IntervalPackets uint64           `json:"interval_packets,omitempty"`
	IntervalBytes   uint64           `json:"interval_bytes,omitempty"`
	PPS             float64          `json:"pps,omitempty"`
	BPS             float64          `json:"bps,omitempty"`
	WorkerPool      *workerPoolStats `json:"worker_pool,omitempty"`
}

type workerPoolStats struct {
	Size           int   `json:"size"`
	Running        int   `json:"running"`
	Free           int   `json:"free"`
	Waiting        int   `json:"waiting"`
	QueueSize      int   `json:"queue_size,omitempty"`
	QueueAvailable int   `json:"queue_available,omitempty"`
	Inflight       int64 `json:"inflight"`
	FastPath       bool  `json:"fast_path"`
}

func newTrafficSummary(elapsed time.Duration) *trafficSummary {
	return &trafficSummary{Uptime: elapsed.Truncate(time.Millisecond).String()}
}

func (s *trafficSummary) add(c *trafficCounter, packets, bytes uint64, interval time.Duration) {
	if c.role != "task" {
		return
	}

	item := taskAggregateStats{
		Task:         c.name,
		TotalPackets: packets,
		TotalBytes:   bytes,
	}

	if interval > 0 {
		lastPackets := c.lastPkt.Swap(packets)
		lastBytes := c.lastB.Swap(bytes)
		intervalPackets := diffCounter(packets, lastPackets)
		intervalBytes := diffCounter(bytes, lastBytes)
		item.IntervalPackets = intervalPackets
		item.IntervalBytes = intervalBytes
		seconds := interval.Seconds()
		if seconds > 0 {
			item.PPS = float64(intervalPackets) / seconds
			item.BPS = float64(intervalBytes) / seconds
		}
	}

	if runtime, ok := lookupTaskRuntimeStats(c.name); ok {
		item.WorkerPool = &workerPoolStats{
			Size:           runtime.PoolSize,
			Running:        runtime.PoolRunning,
			Free:           runtime.PoolFree,
			Waiting:        runtime.PoolWaiting,
			QueueSize:      runtime.QueueSize,
			QueueAvailable: runtime.QueueAvailable,
			Inflight:       runtime.Inflight,
			FastPath:       runtime.FastPath,
		}
	}

	s.Tasks = append(s.Tasks, item)
}

func diffCounter(cur, prev uint64) uint64 {
	if cur >= prev {
		return cur - prev
	}
	return cur
}

func (s *trafficSummary) hasData() bool {
	return len(s.Tasks) > 0
}

func (s *trafficSummary) log() {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		L().Errorw("marshal traffic stats summary failed", "error", err)
		return
	}
	L().Info("traffic stats summary\n" + string(b))
}

func (s *trafficSummary) addRuntimeOnlyTask(task string, runtime TaskRuntimeStats) {
	s.Tasks = append(s.Tasks, taskAggregateStats{
		Task: task,
		WorkerPool: &workerPoolStats{
			Size:           runtime.PoolSize,
			Running:        runtime.PoolRunning,
			Free:           runtime.PoolFree,
			Waiting:        runtime.PoolWaiting,
			QueueSize:      runtime.QueueSize,
			QueueAvailable: runtime.QueueAvailable,
			Inflight:       runtime.Inflight,
			FastPath:       runtime.FastPath,
		},
	})
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
