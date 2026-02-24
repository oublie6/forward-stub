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

func init() {
	trafficStatsIntervalNanos.Store(int64(time.Second))
}

func SetTrafficStatsInterval(d time.Duration) {
	if d <= 0 {
		d = time.Second
	}
	trafficStatsIntervalNanos.Store(int64(d))
}

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

	packets atomic.Uint64
	bytes   atomic.Uint64
	refs    atomic.Int64
}

type TrafficCounter struct {
	hub *trafficStatsHub
	key string
	c   *trafficCounter
}

func (tc *TrafficCounter) AddBytes(n int) {
	if tc == nil || tc.c == nil || n <= 0 {
		return
	}
	tc.c.packets.Add(1)
	tc.c.bytes.Add(uint64(n))
}

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

func AcquireTrafficCounter(msg string, fields ...any) *TrafficCounter {
	return globalTrafficHub.acquire(msg, fields...)
}

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
	c.refs.Store(1)
	h.counters[key] = c
	return &TrafficCounter{hub: h, key: key, c: c}
}

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
	for _, c := range cs {
		pkts := c.packets.Swap(0)
		b := c.bytes.Swap(0)
		if pkts == 0 {
			continue
		}
		args := make([]any, 0, len(c.fields)+10)
		args = append(args, c.fields...)
		args = append(args,
			"interval", interval.String(),
			"packets", pkts,
			"bytes", b,
			"pps", float64(pkts)/sec,
			"mbps", float64(b*8)/sec/1_000_000,
		)
		L().Infow(c.msg, args...)
	}
}

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
