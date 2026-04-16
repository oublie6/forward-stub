// traffic_agg.go 负责“聚合统计日志”主机制：
// 1. 为 receiver / task 等热路径组件提供轻量级计数器句柄；
// 2. 在后台 goroutine 中按固定周期汇总累计值与区间增量；
// 3. 结合 task 运行时状态，输出面向运维与排障的结构化摘要日志；
// 4. 在热重载场景中通过引用计数复用计数器，尽量避免统计断档。
//
// 设计目标是：热路径只做原子加法和少量采样判断，慢路径再做格式化、聚合与日志输出。
package logx

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// trafficStatsIntervalNanos 保存聚合日志输出周期，单位为纳秒。
// 之所以使用 atomic，是为了允许 Init/测试代码在运行期间无锁更新该配置，
// 后台 flush goroutine 读取时无需再持有额外锁。
var trafficStatsIntervalNanos atomic.Int64

// trafficStatsSampleEvery 保存“每 N 条样本命中 1 次”的采样倍率。
// 采样只影响热路径上的计数写入频率，不改变日志线程的聚合方式；
// 命中采样时会按 sample 倍数回填 packets/bytes，保持统计总量近似。
var trafficStatsSampleEvery atomic.Int64

// init 初始化聚合统计模块的默认周期与采样率。
// 生命周期：随进程启动执行一次，后续可被 Init/测试代码覆盖。
func init() {
	trafficStatsIntervalNanos.Store(int64(time.Second))
	trafficStatsSampleEvery.Store(1)
}

// SetTrafficStatsInterval 更新聚合统计日志的刷新周期。
//
// 调用时机：
//   - 通常由日志初始化流程在冷启动阶段调用；
//   - 测试也会通过它缩短周期以验证 flush 行为。
//
// 并发语义：线程安全，可与后台 loop 并发执行；新值会在下次启动/读取周期时生效。
func SetTrafficStatsInterval(d time.Duration) {
	if d <= 0 {
		d = time.Second
	}
	trafficStatsIntervalNanos.Store(int64(d))
}

// SetTrafficStatsSampleEvery 设置热路径采样倍率。
//
// N=1 表示每个样本都计数；N>1 时只有第 N、2N、3N... 个样本真正落到原子计数器，
// 并以 sample 倍数补偿累计值，从而在高流量场景下降低原子写放大。
func SetTrafficStatsSampleEvery(n int) {
	if n <= 0 {
		n = 1
	}
	trafficStatsSampleEvery.Store(int64(n))
}

// trafficStatsSampleN 返回当前生效的采样倍率，并兜底保证最小值为 1。
// 该函数只在创建新 counter 时读取一次，因此已有 counter 不会动态切换采样策略。
func trafficStatsSampleN() uint64 {
	v := trafficStatsSampleEvery.Load()
	if v <= 0 {
		return 1
	}
	return uint64(v)
}

// trafficStatsInterval 返回当前生效的聚合输出周期，并兜底回退到 1 秒。
// 后台 loop 启动时会读取该值，用于决定 ticker 周期。
func trafficStatsInterval() time.Duration {
	n := trafficStatsIntervalNanos.Load()
	if n <= 0 {
		return time.Second
	}
	return time.Duration(n)
}

// TaskRuntimeStats 描述 task 在 flush 时一并附加到统计日志中的运行时快照。
//
// 这些字段不直接来自流量累计器，而是由 task 在 flush 慢路径上按需回调提供，
// 用于把“吞吐统计”和“执行模型/池状态”放到同一条设计说明式日志里。
type TaskRuntimeStats struct {
	// ExecutionModel 是 task 当前执行模型（fastpath/pool/channel）。
	ExecutionModel string
	// PoolSize 是 task 配置的 worker 池容量，仅 pool 模型有意义。
	PoolSize int
	// WorkerPoolRunning 是当前 ants 池中正在执行的 worker 数量。
	WorkerPoolRunning int
	// WorkerPoolFree 是 ants 池空闲 worker 数量，便于判断是否接近饱和。
	WorkerPoolFree int
	// WorkerPoolWaiting 是 ants 当前等待 worker 的请求数。
	WorkerPoolWaiting int
	// ChannelQueueSize 是 channel 模型队列容量上限。
	ChannelQueueSize int
	// ChannelQueueUsed 是 channel 模型当前已使用的队列槽位数。
	ChannelQueueUsed int
	// ChannelQueueAvailable 是 channel 模型当前剩余可用槽位数。
	ChannelQueueAvailable int
	// Inflight 是任务当前仍未完成的包数量，覆盖 fastpath / pool / channel 三种模型。
	Inflight int64
	// RouteSenderMiss 是 RouteSender 指向当前 task 未绑定 sender 的累计次数。
	RouteSenderMiss uint64
	// Async 是 sender 可选暴露的异步队列与 callback 结果统计。
	Async TaskAsyncStats
}

// TaskAsyncStats 描述 task 绑定 sender 的异步发送运行态汇总。
type TaskAsyncStats struct {
	QueueSize      int
	QueueUsed      int
	QueueAvailable int
	Dropped        uint64
	SendErrors     uint64
	SendSuccess    uint64
}

// taskRuntimeStatsMu 保护 taskRuntimeStatsFn 的注册表。
// 这里选择 RWMutex 而不是 sync.Map，是因为 task 数量有限且需要成批快照读取。
var taskRuntimeStatsMu sync.RWMutex

// taskRuntimeStatsFn 保存“task 名称 -> 运行时状态采样函数”的映射。
// task.Start 注册、task.StopGraceful 注销，flush 时读取这份表构造 task 运行态摘要。
var taskRuntimeStatsFn = make(map[string]func() TaskRuntimeStats)

// RegisterTaskRuntimeStats 注册 task 运行时统计回调。
//
// 为什么存在：
// 聚合统计日志不仅要输出字节/包量，还要把 task 当前执行模型、worker 池/队列、inflight
// 一起打出来，帮助维护者判断瓶颈是在入口流量、执行队列还是发送端。
//
// 生命周期：task.Start 调用一次；同名 task 热重载重建时会覆盖旧回调。
func RegisterTaskRuntimeStats(task string, fn func() TaskRuntimeStats) {
	if task == "" || fn == nil {
		return
	}
	taskRuntimeStatsMu.Lock()
	taskRuntimeStatsFn[task] = fn
	taskRuntimeStatsMu.Unlock()
}

// UnregisterTaskRuntimeStats 注销 task 运行时统计回调。
// 调用时机：task.StopGraceful 在真正释放执行资源前调用，避免停机后的幽灵 task 出现在摘要里。
func UnregisterTaskRuntimeStats(task string) {
	if task == "" {
		return
	}
	taskRuntimeStatsMu.Lock()
	delete(taskRuntimeStatsFn, task)
	taskRuntimeStatsMu.Unlock()
}

// lookupTaskRuntimeStats 按 task 名称拉取单条运行时快照。
// 该函数会先在读锁内取出回调，再在锁外执行，避免回调实现阻塞注册表。
func lookupTaskRuntimeStats(task string) (TaskRuntimeStats, bool) {
	taskRuntimeStatsMu.RLock()
	fn := taskRuntimeStatsFn[task]
	taskRuntimeStatsMu.RUnlock()
	if fn == nil {
		return TaskRuntimeStats{}, false
	}
	return fn(), true
}

// listTaskRuntimeStats 返回所有 task 的运行时快照副本。
// flush 使用它补齐“当前没有流量但仍在运行”的 task，避免日志遗漏空闲队列状态。
func listTaskRuntimeStats() map[string]TaskRuntimeStats {
	taskRuntimeStatsMu.RLock()
	fns := make(map[string]func() TaskRuntimeStats, len(taskRuntimeStatsFn))
	for task, fn := range taskRuntimeStatsFn {
		if fn == nil {
			continue
		}
		fns[task] = fn
	}
	taskRuntimeStatsMu.RUnlock()

	out := make(map[string]TaskRuntimeStats, len(fns))
	for task, fn := range fns {
		out[task] = fn()
	}
	return out
}

// trafficCounter 是真正被多个 TrafficCounter 句柄共享的底层累计实体。
//
// 生命周期：
//   - 第一次 AcquireTrafficCounter 时创建；
//   - 后续相同 key 的调用只增加 refs；
//   - 所有句柄 Close 后从 hub 中移除。
//
// 并发语义：
//   - packets/bytes/last*/refs/seen 全部通过 atomic 维护；
//   - 允许多个 goroutine 在热路径并发 AddBytes；
//   - 允许热重载时旧 task Close、新 task 继续复用同一底层实体。
type trafficCounter struct {
	// msg 是最终日志主题文本，主要用于区分不同统计类别。
	msg string
	// fields 保留创建 counter 时的原始 kv 对，便于后续需要扩展更细粒度摘要。
	fields []any
	// role 标识该 counter 归属的组件类别，目前主要关心 task / receiver。
	role string
	// name 是组件名（task 名、receiver 名等），用于聚合归类与日志展示。
	name string
	// key 是 role 下的次级标识，例如 receiver_key / direction。
	key string
	// sample 是该 counter 固定采样倍率，在创建时冻结，避免运行期动态切换引入歧义。
	sample uint64

	// packets / bytes 分别记录累计包数与累计字节数，供 flush 输出总量。
	packets atomic.Uint64
	bytes   atomic.Uint64
	// lastPkt / lastB 记录上一次 flush 时的累计值，用于计算区间增量与 PPS/BPS。
	lastPkt atomic.Uint64
	lastB   atomic.Uint64
	// refs 记录仍有多少逻辑句柄在持有该底层 counter，用于安全复用与回收。
	refs atomic.Int64
	// seen 记录热路径见到的样本总数，仅用于采样判定，不直接对外输出。
	seen atomic.Uint64
}

// TrafficCounter 是暴露给 receiver/task 等调用方的轻量级句柄。
//
// 它本身不持有独立计数状态，而是指向 hub 内共享的 trafficCounter。
// 这样同名 task 在热重载时可以把句柄转移给新实例，继续沿用累计值。
type TrafficCounter struct {
	// hub 指向全局计数中心，Close 时需要回到 hub 里减少引用。
	hub *trafficStatsHub
	// key 是该句柄对应的唯一去重键，用于在 Close 时回收底层实体。
	key string
	// c 指向真正承载原子计数的共享实体。
	c *trafficCounter
	// closed 仅用于句柄级幂等关闭。
	// 它不会阻止其他共享句柄继续 AddBytes，只保证当前句柄不重复 release。
	closed atomic.Bool
}

// AddBytes 在热路径上追加一次流量样本。
//
// 性能语义：
//   - 仅包含 nil/closed 判断、一个 seen 原子自增，以及命中采样后的两次原子加法；
//   - 不做日志格式化、不加锁，是整套聚合统计机制进入数据面的唯一热路径接口。
//
// 线程安全：是。可被多个 goroutine 并发调用。
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

// Close 释放当前句柄对底层统计对象的引用。
//
// 注意：
//   - Close 只影响当前句柄，不会强制停止其他复用句柄计数；
//   - 多次调用是幂等的；
//   - 与 AddBytes 并发时，closed 标记可避免当前句柄在关闭后继续写入。
func (tc *TrafficCounter) Close() {
	if tc == nil || tc.hub == nil || tc.key == "" {
		return
	}
	if !tc.closed.CompareAndSwap(false, true) {
		return
	}
	tc.hub.release(tc.key)
}

// trafficStatsHub 管理全局共享 counter 集合与后台 flush goroutine。
//
// hub 只在 acquire/release/flush 快照阶段持锁；真正的累计值保存在 atomic 字段中，
// 因此热路径不会因为 hub 锁竞争而阻塞。
type trafficStatsHub struct {
	// mu 保护 counters map 的增删与快照读取。
	mu sync.RWMutex
	// counters 保存当前仍被至少一个句柄引用的底层计数器。
	counters map[string]*trafficCounter

	// startOnce 保证后台 flush goroutine 最多启动一次。
	startOnce sync.Once
	// stopCh 预留给测试或未来扩展的停止信号，当前进程常驻模式下通常不会关闭。
	stopCh chan struct{}
}

// globalTrafficHub 是整个进程共享的唯一聚合统计中心。
// receiver / task 等所有组件都通过它获取计数器，从而在同一处按周期输出摘要。
var globalTrafficHub = &trafficStatsHub{
	counters: make(map[string]*trafficCounter),
	stopCh:   make(chan struct{}),
}

// AcquireTrafficCounter 为某条逻辑链路获取一个可复用的聚合统计句柄。
//
// 去重维度由 msg+fields 决定：相同维度会复用同一底层累计值；
// 不同维度则各自独立输出，避免不同组件之间混淆。
func AcquireTrafficCounter(msg string, fields ...any) *TrafficCounter {
	return globalTrafficHub.acquire(msg, fields...)
}

// acquire 是 hub 内部的句柄获取实现。
//
// 调用时会确保后台 flush loop 已启动，然后按 key 查找是否已有同类 counter：
//   - 命中：仅增加 refs，返回新句柄；
//   - 未命中：创建底层 counter，并冻结其采样倍率。
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

// release 归还某个 key 对应的底层 counter 引用。
// 当 refs 归零时会从 hub 中移除，使后续 flush 不再看到该实体。
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

// loop 是后台聚合输出 goroutine。
//
// 生命周期：
//   - 第一次 AcquireTrafficCounter 时惰性启动；
//   - 启动后先执行一次空 flush，确保 uptime 从进程内第一次统计创建时开始计；
//   - 然后按固定 ticker 周期输出摘要。
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

// flush 从 hub 中抓取当前 counter 快照，并生成一次摘要日志。
//
// 关键点：
//   - 只在复制 counters 切片时持读锁，避免日志序列化阻塞 acquire/release；
//   - receiver 目前只贡献累计字节/包数，不输出独立摘要条目；
//   - task 除累计值外，还会拼接 runtimeStats；
//   - 即使 task 暂时没有流量，只要仍注册了 runtimeStats，也会出现在摘要中。
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

// trafficSummary 是单次 flush 输出的聚合日志模型。
// 当前只输出 task 视角摘要，因为它最能串起“收包后是否真正被处理/发送”。
type trafficSummary struct {
	Uptime string               `json:"uptime"`
	Tasks  []taskAggregateStats `json:"tasks"`
}

// taskAggregateStats 描述单个 task 在某次 flush 中的聚合结果。
type taskAggregateStats struct {
	Task            string           `json:"task"`
	TotalPackets    uint64           `json:"total_packets"`
	TotalBytes      uint64           `json:"total_bytes"`
	IntervalPackets uint64           `json:"interval_packets,omitempty"`
	IntervalBytes   uint64           `json:"interval_bytes,omitempty"`
	PPS             float64          `json:"pps,omitempty"`
	BPS             float64          `json:"bps,omitempty"`
	ExecutionModel  string           `json:"execution_model,omitempty"`
	Inflight        int64            `json:"inflight,omitempty"`
	RouteSenderMiss uint64           `json:"route_sender_miss,omitempty"`
	PoolSize        int              `json:"pool_size,omitempty"`
	WorkerPool      *workerPoolStats `json:"worker_pool,omitempty"`
	Channel         *channelStats    `json:"channel,omitempty"`
	Async           *asyncStats      `json:"async,omitempty"`
}

// workerPoolStats 描述 pool 执行模型在 flush 时刻的运行状态。
type workerPoolStats struct {
	Running int `json:"running"`
	Free    int `json:"free"`
	Waiting int `json:"waiting"`
}

// channelStats 描述 channel 执行模型在 flush 时刻的队列状态。
type channelStats struct {
	QueueSize      int `json:"queue_size,omitempty"`
	QueueUsed      int `json:"queue_used,omitempty"`
	QueueAvailable int `json:"queue_available,omitempty"`
}

// asyncStats 描述 sender async 模式的本地队列和异步 callback 结果。
type asyncStats struct {
	QueueSize      int    `json:"queue_size,omitempty"`
	QueueUsed      int    `json:"queue_used,omitempty"`
	QueueAvailable int    `json:"queue_available,omitempty"`
	Dropped        uint64 `json:"dropped,omitempty"`
	SendErrors     uint64 `json:"send_errors,omitempty"`
	SendSuccess    uint64 `json:"send_success,omitempty"`
}

// newTrafficSummary 创建单次 flush 使用的摘要容器。
// uptime 会被截断到毫秒，避免日志中过多噪声小数位。
func newTrafficSummary(elapsed time.Duration) *trafficSummary {
	return &trafficSummary{Uptime: elapsed.Truncate(time.Millisecond).String()}
}

// add 将单个底层 counter 的累计值并入摘要。
//
// 当前只聚合 role=task 的 counter，因为 task 级别最适合作为系统转发结果的观察口；
// receiver counter 仍会累计，但暂不单独出现在输出模型中。
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
		// lastPkt/lastB 通过 Swap 记录上一周期累计值，能在无锁条件下计算区间吞吐。
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
		item.applyRuntime(runtime)
	}

	s.Tasks = append(s.Tasks, item)
}

// diffCounter 计算单调递增计数器的区间增量。
// 若出现回退（例如热重建或测试干预），则直接返回当前值，避免生成负数。
func diffCounter(cur, prev uint64) uint64 {
	if cur >= prev {
		return cur - prev
	}
	return cur
}

// hasData 判断当前摘要是否值得输出。
// 目前只要存在任意 task 条目就输出；空摘要会被静默跳过，减少噪声日志。
func (s *trafficSummary) hasData() bool {
	return len(s.Tasks) > 0
}

// log 将摘要格式化为带缩进的 JSON，并写入 info 日志。
// 这是纯慢路径操作，允许较高格式化成本以换取可读性。
func (s *trafficSummary) log() {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		L().Errorw("流量统计摘要序列化失败", "错误", err)
		return
	}
	L().Info("流量统计摘要\n" + string(b))
}

// addRuntimeOnlyTask 为“当前周期无流量、但 task 仍在运行”的场景补一条摘要记录。
// 这样维护者可以看到空闲 task 的运行态，而不会误以为该 task 已消失。
func (s *trafficSummary) addRuntimeOnlyTask(task string, runtime TaskRuntimeStats) {
	item := taskAggregateStats{Task: task}
	item.applyRuntime(runtime)
	s.Tasks = append(s.Tasks, item)
}

func (t *taskAggregateStats) applyRuntime(runtime TaskRuntimeStats) {
	t.ExecutionModel = runtime.ExecutionModel
	t.Inflight = runtime.Inflight
	t.RouteSenderMiss = runtime.RouteSenderMiss
	switch runtime.ExecutionModel {
	case "pool":
		t.PoolSize = runtime.PoolSize
		t.WorkerPool = &workerPoolStats{
			Running: runtime.WorkerPoolRunning,
			Free:    runtime.WorkerPoolFree,
			Waiting: runtime.WorkerPoolWaiting,
		}
	case "channel":
		t.Channel = &channelStats{
			QueueSize:      runtime.ChannelQueueSize,
			QueueUsed:      runtime.ChannelQueueUsed,
			QueueAvailable: runtime.ChannelQueueAvailable,
		}
	}
	if runtime.Async.QueueSize > 0 || runtime.Async.Dropped > 0 || runtime.Async.SendErrors > 0 || runtime.Async.SendSuccess > 0 {
		t.Async = &asyncStats{
			QueueSize:      runtime.Async.QueueSize,
			QueueUsed:      runtime.Async.QueueUsed,
			QueueAvailable: runtime.Async.QueueAvailable,
			Dropped:        runtime.Async.Dropped,
			SendErrors:     runtime.Async.SendErrors,
			SendSuccess:    runtime.Async.SendSuccess,
		}
	}
}

// parseTrafficMeta 从创建 counter 时的 kv fields 中抽取聚合维度。
// 这些维度会决定日志归类方式，也影响热重载时是否能命中同一共享 counter。
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

// buildTrafficKey 把 msg 与 kv fields 拼成稳定去重键。
//
// 注意：这里假设调用方按固定顺序传入 fields；
// 若顺序不同，即使语义相同也会被视为不同 counter。
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
