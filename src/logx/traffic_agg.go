// traffic_agg.go 提供流量统计聚合器并定期输出指标日志。
package logx

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var trafficStatsIntervalNanos atomic.Int64
var trafficStatsSampleEvery atomic.Int64
var trafficStatsEnableSender atomic.Bool

// init 负责该函数对应的核心逻辑，详见实现细节。
func init() {
	trafficStatsIntervalNanos.Store(int64(time.Second))
	trafficStatsSampleEvery.Store(1)
	trafficStatsEnableSender.Store(true)
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

// SetTrafficStatsEnableSender 控制是否开启 sender 维度统计。
func SetTrafficStatsEnableSender(enabled bool) {
	trafficStatsEnableSender.Store(enabled)
}

// TrafficStatsEnableSender 返回 sender 维度统计是否开启。
func TrafficStatsEnableSender() bool {
	return trafficStatsEnableSender.Load()
}

// trafficStatsInterval 负责该函数对应的核心逻辑，详见实现细节。
func trafficStatsInterval() time.Duration {
	n := trafficStatsIntervalNanos.Load()
	if n <= 0 {
		return time.Second
	}
	return time.Duration(n)
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
			h.flush(time.Since(startedAt))
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
func (h *trafficStatsHub) flush(elapsed time.Duration) {
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
		summary.add(c, pkts, b)
	}
	if summary.hasData() {
		L().Infow("traffic stats summary", summary.fields()...)
	}
}

type trafficSummary struct {
	uptime string
	task   []string
}

func newTrafficSummary(elapsed time.Duration) *trafficSummary {
	return &trafficSummary{uptime: elapsed.Truncate(time.Millisecond).String()}
}

func (s *trafficSummary) add(c *trafficCounter, packets, bytes uint64) {
	line := fmt.Sprintf("%s|key=%s|total_packets=%d|total_bytes=%d", c.name, c.key, packets, bytes)
	if c.name == "" {
		line = fmt.Sprintf("%s|total_packets=%d|total_bytes=%d", c.msg, packets, bytes)
	}
	switch c.role {
	case "task":
		s.task = append(s.task, line)
	}
}

func (s *trafficSummary) hasData() bool {
	return len(s.task) > 0
}

func (s *trafficSummary) fields() []any {
	args := []any{"uptime", s.uptime}
	if len(s.task) > 0 {
		args = append(args, "tasks", s.task)
	}
	return args
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
