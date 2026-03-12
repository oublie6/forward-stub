// update_cache.go 实现运行时缓存的全量替换流程。
package runtime

import (
	"context"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/logx"
	"forward-stub/src/packet"
	"forward-stub/src/pipeline"
	"forward-stub/src/receiver"
	"forward-stub/src/sender"
	"forward-stub/src/task"

	"go.uber.org/zap/zapcore"
)

// UpdateCache 负责将运行时状态切换到新配置。
//
// 当前实现采用“全量替换”策略（先停旧实例，再创建新实例）：
//  1. 逻辑清晰、故障边界小，适合作为稳定基线；
//  2. 避免差量更新时复杂的拓扑依赖与回滚成本；
//  3. 后续若需更高实时性，可在此基础上演进为增量热更新。
type taskPayloadLogOptions struct {
	send bool
	max  int
}

// buildTaskPayloadLogOptions 计算单个 task 的 payload 日志策略。
//
// 规则：
//  1. 是否打印发送 payload 由 task 自身开关 tc.LogPayloadSend 决定；
//  2. 日志截断上限优先使用 task 局部配置；
//  3. 若 task 未配置上限，则回退到全局 logging 配置。
func buildTaskPayloadLogOptions(tc config.TaskConfig, lc config.LoggingConfig) taskPayloadLogOptions {
	maxBytes := payloadLogMaxBytes(tc.PayloadLogMaxBytes, lc.PayloadLogMaxBytes)
	return taskPayloadLogOptions{
		send: tc.LogPayloadSend,
		max:  maxBytes,
	}
}

// payloadLogMaxBytes 解析“局部优先、全局兜底”的日志截断上限。
//
// 当 localMax > 0 时，说明调用方明确指定了上限，直接采用；
// 否则回退到 logging 级别的默认值。
func payloadLogMaxBytes(localMax int, loggingMax int) int {
	if localMax > 0 {
		return localMax
	}
	return loggingMax
}

// UpdateCache 是运行时配置更新入口。
//
// 处理流程：
//  1. 更新全局 payload 池与日志默认参数；
//  2. 尝试一次“仅重启曾经失败的 receiver”；
//  3. 如果当前状态允许，则走业务增量更新；否则执行全量重建；
//  4. 打印任务快照与耗时，便于排障和容量评估。
//
// 注意：这里仅负责“切换运行时对象”，不做配置合法性校验（校验应在更上层完成）。
func UpdateCache(ctx context.Context, st *Store, cfg config.Config) error {
	packet.SetPayloadPoolMaxCachedBytes(cfg.Logging.PayloadPoolMaxCachedBytes)
	st.setPayloadLogDefaultMax(cfg.Logging.PayloadLogMaxBytes)
	lg := logx.L()
	start := time.Now()
	if logx.Enabled(zapcore.InfoLevel) {
		lg.Infow("updating runtime cache", "version", cfg.Version, "receivers", len(cfg.Receivers), "tasks", len(cfg.Tasks), "senders", len(cfg.Senders))
	}

	if st.canApplyBusinessDelta() {
		if err := st.applyBusinessDelta(ctx, cfg); err != nil {
			return err
		}
	} else {
		if err := st.replaceAll(ctx, cfg); err != nil {
			return err
		}
	}

	if logx.Enabled(zapcore.InfoLevel) {
		lg.Infow("active tasks snapshot", "tasks", st.taskSnapshot())
	}

	if logx.Enabled(zapcore.InfoLevel) {
		lg.Infow("runtime cache updated", "version", cfg.Version, "cost", time.Since(start))
	}
	return nil
}

// setPayloadLogDefaultMax 更新 Store 持有的“全局 payload 日志默认截断长度”。
//
// 该值会在 receiver/task 未显式配置最大长度时作为兜底值使用。
func (st *Store) setPayloadLogDefaultMax(max int) {
	st.mu.Lock()
	st.payloadLogDefaultMax = max
	st.mu.Unlock()
}

// loggingPayloadLogMaxBytes 读取当前 Store 的全局 payload 日志截断默认值。
func (st *Store) loggingPayloadLogMaxBytes() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.payloadLogDefaultMax
}

// canApplyBusinessDelta 判断当前是否可走“增量热更新”路径。
//
// 约束：当前必须已有运行态对象（不是冷启动）。
func (st *Store) canApplyBusinessDelta() bool {
	st.mu.RLock()
	defer st.mu.RUnlock()
	if len(st.receivers) == 0 && len(st.senders) == 0 && len(st.tasks) == 0 {
		return false
	}
	return true
}

// replaceAll 执行全量替换：停旧实例、清空索引、重建并启动新实例。
//
// 顺序说明：
//  1. 先 build sender（task 依赖 sender）；
//  2. 再 build task（建立 pipeline + sender + 订阅关系）；
//  3. 生成 dispatch 快照后再启动 receiver，避免切换窗口漏分发。
func (st *Store) replaceAll(ctx context.Context, cfg config.Config) error {
	_ = st.StopAll(ctx)

	compiled, sigsByPipeline, err := st.compilePipelinesWithStageCache(cfg.Pipelines)
	if err != nil {
		return err
	}

	// 一次性替换 store 内部索引，确保新配置以完整快照生效。
	st.mu.Lock()
	st.receivers = make(map[string]*ReceiverState)
	st.senders = make(map[string]*SenderState)
	st.tasks = make(map[string]*TaskState)
	st.pipelines = compiled
	st.pipelineCfg = cfg.Pipelines
	st.pipelineStageSigs = sigsByPipeline
	st.subs = make(map[string]map[string]struct{})
	st.version = cfg.Version
	st.mu.Unlock()
	st.gcUnusedStageCache()
	st.setDispatchSubs(map[string][]*TaskState{})
	st.setRecvPayloadLogOptions(map[string]recvPayloadLogOption{})

	// 1) 构建 senders：任务阶段需要引用 sender 实例。
	for name, sc := range cfg.Senders {
		s, err := buildSender(name, sc, cfg.Logging.Level)
		if err != nil {
			return err
		}
		st.mu.Lock()
		st.senders[name] = &SenderState{Name: name, Cfg: sc, S: s, Refs: 0}
		st.mu.Unlock()
	}

	// 2) 构建 tasks：绑定 pipeline + sender，并建立 receiver 订阅关系。
	for name, tc := range cfg.Tasks {
		pipes := make([]*pipeline.Pipeline, 0, len(tc.Pipelines))
		for _, pn := range tc.Pipelines {
			cp, ok := compiled[pn]
			if !ok {
				return fmt.Errorf("task %s pipeline %s not found", name, pn)
			}
			pipes = append(pipes, cp.P)
		}

		sends := make([]sender.Sender, 0, len(tc.Senders))
		st.mu.Lock()
		for _, sn := range tc.Senders {
			ss, ok := st.senders[sn]
			if !ok {
				st.mu.Unlock()
				return fmt.Errorf("task %s sender %s not found", name, sn)
			}
			ss.Refs++
			sends = append(sends, ss.S)
		}
		st.mu.Unlock()

		logOpts := buildTaskPayloadLogOptions(tc, cfg.Logging)
		tk := &task.Task{
			Name:             name,
			Pipelines:        pipes,
			Senders:          sends,
			PoolSize:         tc.PoolSize,
			FastPath:         tc.FastPath,
			ExecutionModel:   tc.ExecutionModel,
			QueueSize:        tc.QueueSize,
			ChannelQueueSize: tc.ChannelQueueSize,
			LogPayloadSend:   logOpts.send,
			PayloadLogMax:    logOpts.max,
		}
		if err := tk.Start(); err != nil {
			return err
		}

		st.mu.Lock()
		st.tasks[name] = &TaskState{Name: name, Cfg: tc, T: tk}
		for _, rn := range tc.Receivers {
			if _, ok := st.subs[rn]; !ok {
				st.subs[rn] = make(map[string]struct{})
			}
			st.subs[rn][name] = struct{}{}
		}
		st.mu.Unlock()
	}

	// 3) 先生成 dispatch 只读快照，再启动 receivers，避免热更新窗口漏转。
	dispatchSubs := make(map[string][]*TaskState, len(st.subs))
	st.mu.RLock()
	for rn, sub := range st.subs {
		tasks := make([]*TaskState, 0, len(sub))
		for tn := range sub {
			if ts := st.tasks[tn]; ts != nil {
				tasks = append(tasks, ts)
			}
		}
		dispatchSubs[rn] = tasks
	}
	st.mu.RUnlock()
	st.setDispatchSubs(dispatchSubs)

	// 4) 构建并启动 receivers：消息入口最终回调 dispatch。
	startAcks := make([]<-chan struct{}, 0, len(cfg.Receivers))
	for name, rc := range cfg.Receivers {
		r, err := buildReceiver(name, rc, cfg.Logging.Level)
		if err != nil {
			return err
		}
		rs := &ReceiverState{
			Name:           name,
			Cfg:            rc,
			Recv:           r,
			LogPayloadRecv: rc.LogPayloadRecv,
			PayloadLogMax:  payloadLogMaxBytes(rc.PayloadLogMaxBytes, cfg.Logging.PayloadLogMaxBytes),
		}
		st.mu.Lock()
		st.receivers[name] = rs
		if _, ok := st.subs[name]; !ok {
			st.subs[name] = make(map[string]struct{})
		}
		st.mu.Unlock()
		startAcks = append(startAcks, st.startReceiver(ctx, name, r))
	}
	st.waitReceiversStartInvoked(startAcks)
	st.refreshRecvPayloadLogOptions()

	return nil
}

// applyBusinessDelta 按 receiver/sender/pipeline/task 维度执行差量更新。
//
// 策略：
//  1. 先判断各模块是否变化；
//  2. pipeline 变化时先重编译；
//  3. receiver/sender 分别应用增量；
//  4. task 通过“先删后加”处理替换场景，并在必要时复用统计对象；
//  5. 最后统一刷新分发表、阶段引用和垃圾回收。
func (st *Store) applyBusinessDelta(ctx context.Context, cfg config.Config) error {
	st.mu.RLock()
	oldTasks := make(map[string]config.TaskConfig, len(st.tasks))
	for name, ts := range st.tasks {
		oldTasks[name] = ts.Cfg
	}
	oldSenders := senderConfigSnapshot(st.senders)
	oldReceivers := receiverConfigSnapshot(st.receivers)
	oldPipelines := st.pipelineCfg
	st.mu.RUnlock()

	st.mu.Lock()
	st.version = cfg.Version
	st.mu.Unlock()

	receiverAdded, receiverRemoved := splitDeltaWithReplace(oldReceivers, cfg.Receivers)
	senderAdded, senderRemoved := splitDeltaWithReplace(oldSenders, cfg.Senders)
	pipelineAdded, pipelineRemoved := splitDeltaWithReplace(oldPipelines, cfg.Pipelines)
	taskAdded, taskRemoved := splitDeltaWithReplace(oldTasks, cfg.Tasks)
	if len(senderAdded) > 0 || len(senderRemoved) > 0 {
		taskAdded, taskRemoved = expandTaskDeltaForSenderChanges(oldTasks, cfg.Tasks, taskAdded, taskRemoved, senderAdded, senderRemoved)
	}

	receiverChanged := len(receiverAdded) > 0 || len(receiverRemoved) > 0
	senderChanged := len(senderAdded) > 0 || len(senderRemoved) > 0
	pipelineChanged := len(pipelineAdded) > 0 || len(pipelineRemoved) > 0

	if pipelineChanged {
		compiled, sigsByPipeline, err := st.compilePipelinesWithStageCache(cfg.Pipelines)
		if err != nil {
			return err
		}
		st.mu.Lock()
		st.pipelines = compiled
		st.pipelineCfg = cfg.Pipelines
		st.pipelineStageSigs = sigsByPipeline
		st.mu.Unlock()
		st.gcUnusedStageCache()
	}

	if receiverChanged {
		if err := st.applyReceiverDelta(ctx, cfg.Receivers, cfg.Logging.Level); err != nil {
			return err
		}
	}
	if senderChanged {
		if err := st.applySenderDelta(cfg.Senders, cfg.Logging.Level); err != nil {
			return err
		}
	}

	removeSet := make(map[string]struct{}, len(taskRemoved))
	for _, name := range taskRemoved {
		removeSet[name] = struct{}{}
	}
	addSet := make(map[string]struct{}, len(taskAdded))
	for _, name := range taskAdded {
		addSet[name] = struct{}{}
	}

	for _, name := range taskRemoved {
		_, reAdd := addSet[name]
		counter := st.removeTask(name, reAdd)
		if reAdd {
			if err := st.addTask(name, cfg.Tasks[name], cfg.Logging, counter); err != nil {
				return err
			}
		}
	}
	for _, name := range taskAdded {
		if _, already := removeSet[name]; already {
			continue
		}
		if err := st.addTask(name, cfg.Tasks[name], cfg.Logging, nil); err != nil {
			return err
		}
	}

	st.refreshDispatchSubs()
	st.rebuildStageTaskRefs()
	st.gcUnusedSenders()
	st.gcUnusedStageCache()
	if logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow(
			"runtime business delta applied",
			"version", cfg.Version,
			"receiver_added", receiverAdded,
			"receiver_removed", receiverRemoved,
			"sender_added", senderAdded,
			"sender_removed", senderRemoved,
			"pipeline_added", pipelineAdded,
			"pipeline_removed", pipelineRemoved,
			"task_added", taskAdded,
			"task_removed", taskRemoved,
		)
	}
	return nil
}

// splitDeltaWithReplace 计算两份命名配置的差异集合。
//
// 返回值语义：
//   - added: 新增项 + 配置内容发生变化而需要“重建”的项；
//   - removed: 删除项 + 配置内容发生变化而需要“下线旧实例”的项。
//
// 通过“变更视为 remove+add”统一后续处理模型，可减少分支复杂度。
func splitDeltaWithReplace[T any](oldMap, newMap map[string]T) (added []string, removed []string) {
	for name, oldCfg := range oldMap {
		newCfg, ok := newMap[name]
		if !ok {
			removed = append(removed, name)
			continue
		}
		if !reflect.DeepEqual(oldCfg, newCfg) {
			removed = append(removed, name)
			added = append(added, name)
		}
	}
	for name := range newMap {
		if _, ok := oldMap[name]; !ok {
			added = append(added, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func expandTaskDeltaForSenderChanges(
	oldTasks map[string]config.TaskConfig,
	newTasks map[string]config.TaskConfig,
	taskAdded []string,
	taskRemoved []string,
	senderAdded []string,
	senderRemoved []string,
) ([]string, []string) {
	changedSenders := make(map[string]struct{}, len(senderAdded)+len(senderRemoved))
	for _, name := range senderAdded {
		changedSenders[name] = struct{}{}
	}
	for _, name := range senderRemoved {
		changedSenders[name] = struct{}{}
	}
	if len(changedSenders) == 0 {
		return taskAdded, taskRemoved
	}

	addedSet := make(map[string]struct{}, len(taskAdded))
	for _, name := range taskAdded {
		addedSet[name] = struct{}{}
	}
	removedSet := make(map[string]struct{}, len(taskRemoved))
	for _, name := range taskRemoved {
		removedSet[name] = struct{}{}
	}

	for taskName, oldTC := range oldTasks {
		newTC, ok := newTasks[taskName]
		if !ok {
			continue
		}
		if !taskUsesChangedSender(oldTC, changedSenders) && !taskUsesChangedSender(newTC, changedSenders) {
			continue
		}
		if _, ok := removedSet[taskName]; !ok {
			taskRemoved = append(taskRemoved, taskName)
			removedSet[taskName] = struct{}{}
		}
		if _, ok := addedSet[taskName]; !ok {
			taskAdded = append(taskAdded, taskName)
			addedSet[taskName] = struct{}{}
		}
	}

	sort.Strings(taskAdded)
	sort.Strings(taskRemoved)
	return taskAdded, taskRemoved
}

func taskUsesChangedSender(tc config.TaskConfig, changed map[string]struct{}) bool {
	for _, senderName := range tc.Senders {
		if _, ok := changed[senderName]; ok {
			return true
		}
	}
	return false
}

// applySenderDelta 增量更新 sender 集合。
//
// 处理要点：
//  1. 未变更 sender 直接复用；
//  2. 变更项先创建新 sender，再替换 Store 中引用，最后关闭旧 sender；
//  3. 对已不存在且引用计数为 0 的 sender 做回收。
func (st *Store) applySenderDelta(next map[string]config.SenderConfig, gnetLogLevel string) error {
	st.mu.RLock()
	oldStates := make(map[string]*SenderState, len(st.senders))
	for n, ss := range st.senders {
		oldStates[n] = ss
	}
	st.mu.RUnlock()

	for name, sc := range next {
		old, ok := oldStates[name]
		if ok && reflect.DeepEqual(old.Cfg, sc) {
			continue
		}
		s, err := buildSender(name, sc, gnetLogLevel)
		if err != nil {
			return err
		}
		st.mu.Lock()
		refs := 0
		if cur, ok := st.senders[name]; ok {
			refs = cur.Refs
		}
		st.senders[name] = &SenderState{Name: name, Cfg: sc, S: s, Refs: refs}
		st.mu.Unlock()
		if ok {
			_ = old.S.Close(context.Background())
		}
	}

	st.mu.Lock()
	for name, old := range st.senders {
		if _, ok := next[name]; ok {
			continue
		}
		if old.Refs == 0 {
			delete(st.senders, name)
			go old.S.Close(context.Background())
		}
	}
	st.mu.Unlock()
	return nil
}

// applyReceiverDelta 增量更新 receiver 集合。
//
// 处理要点：
//  1. 对新增或变更 receiver 先构建并启动新实例，再停止旧实例；
//  2. 清理已删除 receiver 的订阅与运行时统计；
//  3. 最后刷新 receiver payload 日志配置快照。
func (st *Store) applyReceiverDelta(ctx context.Context, next map[string]config.ReceiverConfig, gnetLogLevel string) error {
	st.mu.RLock()
	oldStates := make(map[string]*ReceiverState, len(st.receivers))
	for n, rs := range st.receivers {
		oldStates[n] = rs
	}
	st.mu.RUnlock()

	for name, rc := range next {
		old, ok := oldStates[name]
		if ok && reflect.DeepEqual(old.Cfg, rc) {
			continue
		}
		r, err := buildReceiver(name, rc, gnetLogLevel)
		if err != nil {
			return err
		}
		rs := &ReceiverState{
			Name:           name,
			Cfg:            rc,
			Recv:           r,
			LogPayloadRecv: rc.LogPayloadRecv,
			PayloadLogMax:  payloadLogMaxBytes(rc.PayloadLogMaxBytes, st.loggingPayloadLogMaxBytes()),
		}
		st.mu.Lock()
		st.receivers[name] = rs
		if _, ok := st.subs[name]; !ok {
			st.subs[name] = make(map[string]struct{})
		}
		st.mu.Unlock()
		startAck := st.startReceiver(ctx, name, r)
		<-startAck
		if ok && old != nil && old.Recv != nil {
			_ = old.Recv.Stop(ctx)
		}
	}

	st.mu.Lock()
	for name, rs := range st.receivers {
		if _, ok := next[name]; ok {
			continue
		}
		delete(st.receivers, name)
		delete(st.subs, name)
		go rs.Recv.Stop(ctx)
	}
	st.mu.Unlock()
	st.refreshRecvPayloadLogOptions()
	return nil
}

// startReceiver 异步启动单个 receiver。
//
// receiver 回调统一进入 dispatch，负责 fan-out 到相关 task。
func (st *Store) startReceiver(ctx context.Context, name string, r receiver.Receiver) <-chan struct{} {
	started := make(chan struct{})
	go func(r receiver.Receiver, rn string, ack chan<- struct{}) {
		close(ack)
		err := r.Start(ctx, func(pkt *packet.Packet) { dispatch(ctx, st, rn, pkt) })
		if err != nil {
			logx.L().Errorw("receiver stopped with error", "receiver", rn, "error", err)
		}
	}(r, name, started)
	return started
}

func (st *Store) waitReceiversStartInvoked(started []<-chan struct{}) {
	for _, ack := range started {
		<-ack
	}
}

// addTask 创建并接入一个任务。
//
// 步骤：
//  1. 解析并绑定 pipelines；
//  2. 绑定 senders 并增加 sender 引用计数；
//  3. 按配置创建 task 并启动；
//  4. 更新 task 索引、stage 引用和 receiver 订阅；
//  5. 刷新 dispatch 快照使新 task 立即可接收流量。
func (st *Store) addTask(name string, tc config.TaskConfig, lc config.LoggingConfig, counter *logx.TrafficCounter) error {
	st.mu.RLock()
	compiled := st.pipelines
	st.mu.RUnlock()

	pipes := make([]*pipeline.Pipeline, 0, len(tc.Pipelines))
	for _, pn := range tc.Pipelines {
		cp, ok := compiled[pn]
		if !ok {
			return fmt.Errorf("task %s pipeline %s not found", name, pn)
		}
		pipes = append(pipes, cp.P)
	}

	sends := make([]sender.Sender, 0, len(tc.Senders))
	st.mu.Lock()
	for _, sn := range tc.Senders {
		ss, ok := st.senders[sn]
		if !ok {
			st.mu.Unlock()
			return fmt.Errorf("task %s sender %s not found", name, sn)
		}
		ss.Refs++
		sends = append(sends, ss.S)
	}
	st.mu.Unlock()

	logOpts := buildTaskPayloadLogOptions(tc, lc)
	tk := &task.Task{
		Name:             name,
		Pipelines:        pipes,
		Senders:          sends,
		PoolSize:         tc.PoolSize,
		FastPath:         tc.FastPath,
		ExecutionModel:   tc.ExecutionModel,
		QueueSize:        tc.QueueSize,
		ChannelQueueSize: tc.ChannelQueueSize,
		LogPayloadSend:   logOpts.send,
		PayloadLogMax:    logOpts.max,
	}
	if counter != nil {
		tk.ReuseTrafficCounter(counter)
	}
	if err := tk.Start(); err != nil {
		if counter != nil {
			counter.Close()
		}
		return err
	}

	st.mu.Lock()
	st.tasks[name] = &TaskState{Name: name, Cfg: tc, T: tk}
	st.retainTaskStageRefsLocked(name, tc.Pipelines)
	for _, rn := range tc.Receivers {
		if _, ok := st.subs[rn]; !ok {
			st.subs[rn] = make(map[string]struct{})
		}
		st.subs[rn][name] = struct{}{}
	}
	st.mu.Unlock()
	st.refreshDispatchSubs()
	return nil
}

// removeTask 从运行态移除任务并释放关联引用。
//
// 当 preserveCounter=true 时，会把 task 的发送统计对象分离出来并返回，
// 以便同名任务重建时继续复用，避免统计断档。
func (st *Store) removeTask(name string, preserveCounter bool) *logx.TrafficCounter {
	st.mu.Lock()
	ts := st.tasks[name]
	if ts != nil {
		delete(st.tasks, name)
		st.releaseTaskStageRefsLocked(name, ts.Cfg.Pipelines)
		for _, rn := range ts.Cfg.Receivers {
			if sub, ok := st.subs[rn]; ok {
				delete(sub, name)
			}
		}
		for _, sn := range ts.Cfg.Senders {
			if ss, ok := st.senders[sn]; ok {
				ss.Refs--
			}
		}
	}
	st.mu.Unlock()
	st.refreshDispatchSubs()
	var counter *logx.TrafficCounter
	if ts != nil {
		if preserveCounter {
			counter = ts.T.DetachTrafficCounter()
		}
		ts.T.StopGraceful()
	}
	return counter
}

// retainTaskStageRefsLocked 为 task 关联的所有 pipeline stage 增加引用计数。
//
// 调用方必须持有 st.mu（写锁），该函数不自行加锁。
func (st *Store) retainTaskStageRefsLocked(taskName string, pipelines []string) {
	for _, pn := range pipelines {
		sigs := st.pipelineStageSigs[pn]
		for _, sig := range sigs {
			entry := st.stageCache[sig]
			if entry == nil {
				continue
			}
			if entry.Tasks == nil {
				entry.Tasks = make(map[string]struct{})
			}
			if _, ok := entry.Tasks[taskName]; ok {
				continue
			}
			entry.Tasks[taskName] = struct{}{}
			entry.TaskRefs++
		}
	}
}

// releaseTaskStageRefsLocked 释放 task 对 stage cache 的引用。
//
// 调用方必须持有 st.mu（写锁），该函数不自行加锁。
func (st *Store) releaseTaskStageRefsLocked(taskName string, pipelines []string) {
	for _, pn := range pipelines {
		sigs := st.pipelineStageSigs[pn]
		for _, sig := range sigs {
			entry := st.stageCache[sig]
			if entry == nil || entry.Tasks == nil {
				continue
			}
			if _, ok := entry.Tasks[taskName]; !ok {
				continue
			}
			delete(entry.Tasks, taskName)
			entry.TaskRefs--
			if entry.TaskRefs <= 0 {
				entry.TaskRefs = 0
			}
		}
	}
}

// gcUnusedStageCache 清理没有任何 task 引用的 stage cache 条目。
func (st *Store) gcUnusedStageCache() {
	st.mu.Lock()
	defer st.mu.Unlock()
	for sig, entry := range st.stageCache {
		if entry == nil {
			delete(st.stageCache, sig)
			continue
		}
		if entry.TaskRefs > 0 {
			continue
		}
		delete(st.stageCache, sig)
	}
}

// rebuildStageTaskRefs 基于当前 tasks 全量重建 stage 引用计数。
//
// 该方法用于在复杂增量变更后做一次“纠偏”，保证引用计数一致。
func (st *Store) rebuildStageTaskRefs() {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, entry := range st.stageCache {
		if entry == nil {
			continue
		}
		entry.TaskRefs = 0
		entry.Tasks = make(map[string]struct{})
	}
	for taskName, ts := range st.tasks {
		for _, pn := range ts.Cfg.Pipelines {
			sigs := st.pipelineStageSigs[pn]
			for _, sig := range sigs {
				entry := st.stageCache[sig]
				if entry == nil {
					continue
				}
				if _, ok := entry.Tasks[taskName]; ok {
					continue
				}
				entry.Tasks[taskName] = struct{}{}
				entry.TaskRefs++
			}
		}
	}
}

// refreshDispatchSubs 重建 receiver -> task 的只读分发表快照。
//
// dispatch 读取该快照进行无锁分发，避免热路径频繁争用 Store 主锁。
func (st *Store) refreshDispatchSubs() {
	dispatchSubs := make(map[string][]*TaskState)
	st.mu.RLock()
	for rn, sub := range st.subs {
		tasks := make([]*TaskState, 0, len(sub))
		for tn := range sub {
			if ts := st.tasks[tn]; ts != nil {
				tasks = append(tasks, ts)
			}
		}
		dispatchSubs[rn] = tasks
	}
	st.mu.RUnlock()
	st.setDispatchSubs(dispatchSubs)
}

// gcUnusedSenders 回收当前引用计数为 0 的 sender 实例。
func (st *Store) gcUnusedSenders() {
	unused := make([]sender.Sender, 0)
	st.mu.Lock()
	for name, ss := range st.senders {
		if ss.Refs > 0 {
			continue
		}
		unused = append(unused, ss.S)
		delete(st.senders, name)
	}
	st.mu.Unlock()
	for _, s := range unused {
		_ = s.Close(context.Background())
	}
}

// taskSnapshot 生成任务拓扑与执行模型的可观测快照，用于日志输出。
func (st *Store) taskSnapshot() []map[string]any {
	st.mu.RLock()
	defer st.mu.RUnlock()
	out := make([]map[string]any, 0, len(st.tasks))
	for name, ts := range st.tasks {
		out = append(out, map[string]any{
			"task":            name,
			"receivers":       ts.Cfg.Receivers,
			"pipelines":       ts.Cfg.Pipelines,
			"senders":         ts.Cfg.Senders,
			"execution_model": ts.T.ExecutionModel,
			"queue_size":      ts.T.QueueSize,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["task"].(string) < out[j]["task"].(string)
	})
	return out
}

// receiverConfigSnapshot 提取 receiver 配置快照，便于后续做差异比较。
func receiverConfigSnapshot(m map[string]*ReceiverState) map[string]config.ReceiverConfig {
	out := make(map[string]config.ReceiverConfig, len(m))
	for n, s := range m {
		out[n] = s.Cfg
	}
	return out
}

// senderConfigSnapshot 提取 sender 配置快照，便于后续做差异比较。
func senderConfigSnapshot(m map[string]*SenderState) map[string]config.SenderConfig {
	out := make(map[string]config.SenderConfig, len(m))
	for n, s := range m {
		out[n] = s.Cfg
	}
	return out
}

// dispatch 将单个输入包 fan-out 到订阅当前 receiver 的所有任务。
//
// 性能关键点：
//  1. 在锁内仅完成任务列表快照，避免长时间持锁；
//  2. 多订阅者时对每个任务都 Clone，彻底隔离任务间生命周期；
//  3. 单订阅者直接复用原始包，减少一次额外复制。
func dispatch(ctx context.Context, st *Store, receiverName string, pkt *packet.Packet) {
	tasks := st.getDispatchTasks(receiverName)
	if opt, ok := st.getRecvPayloadLogOption(receiverName); ok && opt.enabled {
		logReceiverPayload(receiverName, pkt, opt.max)
	}

	if len(tasks) == 0 {
		pkt.Release()
		return
	}

	// 仅有一个订阅任务时直接复用原始包，避免无意义二次复制。
	if len(tasks) == 1 {
		tasks[0].T.Handle(ctx, pkt)
		return
	}

	// 多任务场景下，为避免共享 packet 带来的释放时序竞争，
	// 先为其余任务逐个 Clone，再把原始包交给首个任务，避免额外中间切片。
	for _, ts := range tasks[1:] {
		cp := pkt.Clone()
		ts.T.Handle(ctx, cp)
	}
	first := tasks[0]
	first.T.Handle(ctx, pkt)
}

func (st *Store) refreshRecvPayloadLogOptions() {
	snapshot := make(map[string]recvPayloadLogOption)
	st.mu.RLock()
	for name, rs := range st.receivers {
		if rs == nil {
			continue
		}
		snapshot[name] = recvPayloadLogOption{enabled: rs.LogPayloadRecv, max: rs.PayloadLogMax}
	}
	st.mu.RUnlock()
	st.setRecvPayloadLogOptions(snapshot)
}

// logReceiverPayload 按 receiver 侧日志策略输出 payload 观测日志。
//
// 注意：
//  1. 仅在 info 级别启用；
//  2. payload 内容按 maxBytes 截断，避免超大包放大日志开销。
func logReceiverPayload(receiver string, pkt *packet.Packet, maxBytes int) {
	if pkt == nil || !logx.Enabled(zapcore.InfoLevel) {
		return
	}
	logx.L().Infow("receiver payload recv",
		"receiver", receiver,
		"kind", pkt.Kind,
		"payload_len", len(pkt.Payload),
		"payload_hex", payloadHex(pkt.Payload, maxBytes),
		"transfer_id", pkt.Meta.TransferID,
		"offset", pkt.Meta.Offset,
		"total_size", pkt.Meta.TotalSize,
		"eof", pkt.Meta.EOF,
	)
}

// payloadHex 将 payload 编码为十六进制字符串，并在超过上限时截断。
func payloadHex(b []byte, max int) string {
	if len(b) == 0 {
		return ""
	}
	if max <= 0 {
		max = config.DefaultPayloadLogMaxBytes
	}
	if len(b) > max {
		return hex.EncodeToString(b[:max]) + "...(truncated)"
	}
	return hex.EncodeToString(b)
}

// buildReceiver 负责该函数对应的核心逻辑，详见实现细节。
func buildReceiver(name string, rc config.ReceiverConfig, gnetLogLevel string) (receiver.Receiver, error) {
	multicore := config.DefaultReceiverMulticore
	if rc.Multicore != nil {
		multicore = *rc.Multicore
	}
	numEventLoop := rc.NumEventLoop
	if numEventLoop <= 0 {
		numEventLoop = config.DefaultReceiverNumEventLoop
	}
	switch rc.Type {
	case "udp_gnet":
		return receiver.NewGnetUDP(name, rc.Listen, multicore, numEventLoop, rc.ReadBufferCap, gnetLogLevel), nil
	case "tcp_gnet":
		var fr receiver.Framer
		switch rc.Frame {
		case "":
			fr = nil
		case "u16be":
			fr = receiver.U16BEFramer{}
		default:
			return nil, fmt.Errorf("receiver %s unknown frame %s", name, rc.Frame)
		}
		return receiver.NewGnetTCP(name, rc.Listen, multicore, numEventLoop, rc.ReadBufferCap, fr, gnetLogLevel), nil
	case "kafka":
		return receiver.NewKafkaReceiver(name, rc)
	case "sftp":
		return receiver.NewSFTPReceiver(name, rc)
	default:
		return nil, fmt.Errorf("receiver %s unknown type %s", name, rc.Type)
	}
}

// buildSender 负责该函数对应的核心逻辑，详见实现细节。
func buildSender(name string, sc config.SenderConfig, gnetLogLevel string) (sender.Sender, error) {
	conc := sc.Concurrency
	if conc <= 0 {
		conc = config.DefaultSenderConcurrency
	}
	switch sc.Type {
	case "udp_unicast":
		if sc.LocalPort <= 0 {
			return nil, fmt.Errorf("sender %s udp_unicast requires local_port", name)
		}
		return sender.NewUDPUnicastSender(name, sc.LocalIP, sc.LocalPort, sc.Remote, conc)
	case "udp_multicast":
		if sc.LocalPort <= 0 {
			return nil, fmt.Errorf("sender %s udp_multicast requires local_port", name)
		}
		return sender.NewUDPMulticastSender(name, sc.LocalIP, sc.LocalPort, sc.Remote, sc.Iface, sc.TTL, sc.Loop, conc)
	case "tcp_gnet":
		with := false
		switch sc.Frame {
		case "", "none":
			with = false
		case "u16be":
			with = true
		default:
			return nil, fmt.Errorf("sender %s unknown frame %s", name, sc.Frame)
		}
		return sender.NewGnetTCPSender(name, sc.Remote, with, conc, gnetLogLevel)
	case "kafka":
		return sender.NewKafkaSender(name, sc)
	case "sftp":
		return sender.NewSFTPSender(name, sc)
	default:
		return nil, fmt.Errorf("sender %s unknown type %s", name, sc.Type)
	}
}
