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

// init 负责该函数对应的核心逻辑，详见实现细节。
func init() {
	trafficStatsIntervalNanos.Store(int64(time.Second))
}

// SetTrafficStatsInterval 负责该函数对应的核心逻辑，详见实现细节。
func SetTrafficStatsInterval(d time.Duration) {
	if d <= 0 {
		d = time.Second
	}
	trafficStatsIntervalNanos.Store(int64(d))
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

	packets atomic.Uint64
	bytes   atomic.Uint64
	refs    atomic.Int64
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
	tc.c.packets.Add(1)
	tc.c.bytes.Add(uint64(n))
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
	for {
		interval := trafficStatsInterval()
		t := time.NewTimer(interval)
		select {
		case <-t.C:
			h.flush(interval)
		case <-h.stopCh:
			if !t.Stop() {
				<-t.C
			}
			return
		}
	}
}

// flush 负责该函数对应的核心逻辑，详见实现细节。
func (h *trafficStatsHub) flush(interval time.Duration) {
	h.mu.RLock()
	cs := make([]*trafficCounter, 0, len(h.counters))
	for _, c := range h.counters {
		cs = append(cs, c)
	}
	h.mu.RUnlock()
	if len(cs) == 0 {
		return
	}
	sec := interval.Seconds()
	summary := newTrafficSummary(interval)
	for _, c := range cs {
		pkts := c.packets.Swap(0)
		b := c.bytes.Swap(0)
		if pkts == 0 {
			continue
		}
		summary.add(c, pkts, b, sec)
	}
	if summary.hasData() {
		L().Infow("traffic stats summary", summary.fields()...)
	}
}

type trafficSummary struct {
	interval string
	receiver []string
	task     []string
	sender   []string
	other    []string
}

func newTrafficSummary(interval time.Duration) *trafficSummary {
	return &trafficSummary{interval: interval.String()}
}

func (s *trafficSummary) add(c *trafficCounter, packets, bytes uint64, sec float64) {
	line := fmt.Sprintf("%s|key=%s|packets=%d|bytes=%d|pps=%.2f|mbps=%.2f", c.name, c.key, packets, bytes, float64(packets)/sec, float64(bytes*8)/sec/1_000_000)
	if c.name == "" {
		line = fmt.Sprintf("%s|packets=%d|bytes=%d|pps=%.2f|mbps=%.2f", c.msg, packets, bytes, float64(packets)/sec, float64(bytes*8)/sec/1_000_000)
	}
	switch c.role {
	case "receiver":
		s.receiver = append(s.receiver, line)
	case "task":
		s.task = append(s.task, line)
	case "sender":
		s.sender = append(s.sender, line)
	default:
		s.other = append(s.other, line)
	}
}

func (s *trafficSummary) hasData() bool {
	return len(s.receiver) > 0 || len(s.task) > 0 || len(s.sender) > 0 || len(s.other) > 0
}

func (s *trafficSummary) fields() []any {
	args := []any{"interval", s.interval}
	if len(s.receiver) > 0 {
		args = append(args, "receivers", s.receiver)
	}
	if len(s.task) > 0 {
		args = append(args, "tasks", s.task)
	}
	if len(s.sender) > 0 {
		args = append(args, "senders", s.sender)
	}
	if len(s.other) > 0 {
		args = append(args, "others", s.other)
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
		case "receiver_key", "sender_key":
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
