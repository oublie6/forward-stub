// Package runtime 实现运行时缓存的全量替换、增量更新与热路径分发逻辑。
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

// taskPayloadLogOptions 描述单个任务生效后的 payload 日志策略。
type taskPayloadLogOptions struct {
	// send 表示任务发送阶段是否输出 payload 摘要日志。
	send bool
	// max 是 payload 摘要的最大截断字节数；小于等于 0 时调用方应继续回退默认值。
	max int
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
//  2. 根据当前 Store 是否已有运行态对象，选择增量更新或全量重建；
//  3. 在切换完成后输出任务快照与耗时，便于排障和容量评估。
//
// 注意：这里仅负责“切换运行时对象”，不做配置合法性校验（校验应在更上层完成）。
func UpdateCache(ctx context.Context, st *Store, cfg config.Config) error {
	packet.SetPayloadPoolMaxCachedBytes(cfg.Logging.PayloadPoolMaxCachedBytes)
	st.setPayloadLogDefaultMax(cfg.Logging.PayloadLogMaxBytes)
	lg := logx.L()
	start := time.Now()
	if logx.Enabled(zapcore.InfoLevel) {
		lg.Infow("开始更新运行时缓存", "配置版本", cfg.Version, "接收端数量", len(cfg.Receivers), "任务数量", len(cfg.Tasks), "发送端数量", len(cfg.Senders))
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
		lg.Infow("运行时任务快照", "任务列表", st.taskSnapshot())
	}

	if logx.Enabled(zapcore.InfoLevel) {
		lg.Infow("运行时缓存更新完成", "配置版本", cfg.Version, "耗时", time.Since(start))
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

// loggingPayloadLogMaxBytes 返回当前 Store 生效的全局 payload 日志默认截断长度。
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
	if logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow("开始全量重建运行时组件")
	}
	_ = st.StopAll(ctx)

	compiled, sigsByPipeline, err := st.compilePipelinesWithStageCache(cfg.Pipelines)
	if err != nil {
		return err
	}
	if logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow("流水线编译完成", "流水线数量", len(compiled))
	}

	// 一次性替换 store 内部索引，确保新配置以完整快照生效。
	st.mu.Lock()
	st.receivers = make(map[string]*ReceiverState)
	st.selectors = cloneSelectorConfigMap(cfg.Selectors)
	st.taskSets = cloneTaskSetMap(cfg.TaskSets)
	st.senders = make(map[string]*SenderState)
	st.tasks = make(map[string]*TaskState)
	st.pipelines = compiled
	st.pipelineCfg = cfg.Pipelines
	st.pipelineStageSigs = sigsByPipeline
	st.mu.Unlock()
	st.gcUnusedStageCache()

	// 1) 构建 senders：任务阶段需要引用 sender 实例。
	if logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow("开始初始化发送端", "发送端数量", len(cfg.Senders))
	}
	for name, sc := range cfg.Senders {
		s, err := buildSender(name, sc, cfg.Logging.Level)
		if err != nil {
			return err
		}
		st.mu.Lock()
		st.senders[name] = &SenderState{Name: name, Cfg: sc, S: s, Refs: 0}
		st.mu.Unlock()
	}
	if logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow("发送端初始化完成", "发送端数量", len(cfg.Senders))
	}

	// 2) 构建 tasks：绑定 pipeline + sender。
	if logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow("开始初始化任务", "任务数量", len(cfg.Tasks))
	}
	for name, tc := range cfg.Tasks {
		if err := st.addTask(name, tc, cfg.Logging, nil); err != nil {
			return err
		}
	}
	if logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow("任务初始化完成", "任务数量", len(cfg.Tasks))
	}

	if err := st.rebuildReceiverSelectors(); err != nil {
		return err
	}
	if logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow("选择器编译完成", "选择器数量", len(cfg.Selectors), "任务集数量", len(cfg.TaskSets))
	}

	// 3) 构建 receivers 并挂载 selector。
	if logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow("开始初始化接收端", "接收端数量", len(cfg.Receivers))
	}
	receiverStates := make([]*ReceiverState, 0, len(cfg.Receivers))
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
			SelectorName:   rc.Selector,
		}
		st.mu.Lock()
		st.receivers[name] = rs
		st.mu.Unlock()
		receiverStates = append(receiverStates, rs)
	}
	if err := st.rebuildReceiverSelectors(); err != nil {
		return err
	}
	// 4) 启动 receivers：消息入口最终回调 dispatch。
	if logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow("开始启动接收端", "接收端数量", len(receiverStates))
	}
	startAcks := make([]<-chan struct{}, 0, len(receiverStates))
	for _, rs := range receiverStates {
		startAcks = append(startAcks, st.startReceiver(ctx, rs))
	}
	st.waitReceiversStartInvoked(startAcks)
	if logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow("接收端启动完成", "接收端数量", len(receiverStates))
	}
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
	oldSelectors := cloneSelectorConfigMap(st.selectors)
	oldTaskSets := cloneTaskSetMap(st.taskSets)
	oldPipelines := st.pipelineCfg
	st.mu.RUnlock()

	plan := planBusinessDelta(oldReceivers, oldSelectors, oldTaskSets, oldSenders, oldPipelines, oldTasks, cfg)
	receiverAdded, receiverRemoved := plan.receiverAdded, plan.receiverRemoved
	selectorAdded, selectorRemoved := plan.selectorAdded, plan.selectorRemoved
	taskSetAdded, taskSetRemoved := plan.taskSetAdded, plan.taskSetRemoved
	senderAdded, senderRemoved := plan.senderAdded, plan.senderRemoved
	pipelineAdded, pipelineRemoved := plan.pipelineAdded, plan.pipelineRemoved
	taskAdded, taskRemoved := plan.taskAdded, plan.taskRemoved

	receiverChanged := len(receiverAdded) > 0 || len(receiverRemoved) > 0
	selectorChanged := len(selectorAdded) > 0 || len(selectorRemoved) > 0
	taskSetChanged := len(taskSetAdded) > 0 || len(taskSetRemoved) > 0
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
	if selectorChanged || taskSetChanged {
		st.mu.Lock()
		st.selectors = cloneSelectorConfigMap(cfg.Selectors)
		st.taskSets = cloneTaskSetMap(cfg.TaskSets)
		st.mu.Unlock()
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

	st.rebuildStageTaskRefs()
	st.gcUnusedSenders()
	st.gcUnusedStageCache()
	if selectorChanged || taskSetChanged || receiverChanged || len(taskAdded) > 0 || len(taskRemoved) > 0 {
		if err := st.rebuildReceiverSelectors(); err != nil {
			return err
		}
	}
	if logx.Enabled(zapcore.InfoLevel) {
		logx.L().Infow(
			"业务配置增量更新完成",
			"配置版本", cfg.Version,
			"新增接收端", receiverAdded,
			"删除接收端", receiverRemoved,
			"新增选择器", selectorAdded,
			"删除选择器", selectorRemoved,
			"新增任务集", taskSetAdded,
			"删除任务集", taskSetRemoved,
			"新增发送端", senderAdded,
			"删除发送端", senderRemoved,
			"新增流水线", pipelineAdded,
			"删除流水线", pipelineRemoved,
			"新增任务", taskAdded,
			"删除任务", taskRemoved,
		)
	}
	return nil
}

// businessDeltaPlan 汇总各类配置对象在一次业务更新中的新增/删除结果。
//
// 其中“修改”会被上游统一展开为 remove+add，因此这里不单独维护 modified 字段。
type businessDeltaPlan struct {
	// receiverAdded 记录需要新增或重建的 receiver 名称集合。
	receiverAdded []string
	// receiverRemoved 记录需要下线的旧 receiver 名称集合。
	receiverRemoved []string
	// selectorAdded 记录需要新增或重建的 selector 名称集合。
	selectorAdded []string
	// selectorRemoved 记录需要删除的 selector 名称集合。
	selectorRemoved []string
	// taskSetAdded 记录需要新增或重建的 task_set 名称集合。
	taskSetAdded []string
	// taskSetRemoved 记录需要删除的 task_set 名称集合。
	taskSetRemoved []string
	// senderAdded 记录需要新增或重建的 sender 名称集合。
	senderAdded []string
	// senderRemoved 记录需要下线的旧 sender 名称集合。
	senderRemoved []string
	// pipelineAdded 记录需要新增或重编译的 pipeline 名称集合。
	pipelineAdded []string
	// pipelineRemoved 记录需要删除的旧 pipeline 名称集合。
	pipelineRemoved []string
	// taskAdded 记录需要新增或重建的 task 名称集合。
	taskAdded []string
	// taskRemoved 记录需要下线的旧 task 名称集合。
	taskRemoved []string
}

// planBusinessDelta 根据旧运行态快照和新配置，生成一次业务更新需要执行的差量计划。
func planBusinessDelta(
	oldReceivers map[string]config.ReceiverConfig,
	oldSelectors map[string]config.SelectorConfig,
	oldTaskSets map[string][]string,
	oldSenders map[string]config.SenderConfig,
	oldPipelines map[string][]config.StageConfig,
	oldTasks map[string]config.TaskConfig,
	cfg config.Config,
) businessDeltaPlan {
	receiverAdded, receiverRemoved := splitDeltaWithReplace(oldReceivers, cfg.Receivers)
	selectorAdded, selectorRemoved := splitDeltaWithReplace(oldSelectors, cfg.Selectors)
	taskSetAdded, taskSetRemoved := splitDeltaWithReplace(oldTaskSets, cfg.TaskSets)
	senderAdded, senderRemoved := splitDeltaWithReplace(oldSenders, cfg.Senders)
	pipelineAdded, pipelineRemoved := splitDeltaWithReplace(oldPipelines, cfg.Pipelines)
	taskAdded, taskRemoved := splitDeltaWithReplace(oldTasks, cfg.Tasks)
	if len(senderAdded) > 0 || len(senderRemoved) > 0 {
		taskAdded, taskRemoved = expandTaskDeltaForSenderChanges(oldTasks, cfg.Tasks, taskAdded, taskRemoved, senderAdded, senderRemoved)
	}
	if len(pipelineAdded) > 0 || len(pipelineRemoved) > 0 {
		taskAdded, taskRemoved = expandTaskDeltaForPipelineChanges(oldTasks, cfg.Tasks, taskAdded, taskRemoved, pipelineAdded, pipelineRemoved)
	}
	return businessDeltaPlan{
		receiverAdded:   receiverAdded,
		receiverRemoved: receiverRemoved,
		selectorAdded:   selectorAdded,
		selectorRemoved: selectorRemoved,
		taskSetAdded:    taskSetAdded,
		taskSetRemoved:  taskSetRemoved,
		senderAdded:     senderAdded,
		senderRemoved:   senderRemoved,
		pipelineAdded:   pipelineAdded,
		pipelineRemoved: pipelineRemoved,
		taskAdded:       taskAdded,
		taskRemoved:     taskRemoved,
	}
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

// expandTaskDeltaForSenderChanges 把 sender 变化扩散到依赖它们的任务集合中。
//
// 即使任务本身配置未改，只要引用了发生变化的 sender，也必须被视为 remove+add。
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

// taskUsesChangedSender 判断任务是否依赖任一已变化的 sender。
func taskUsesChangedSender(tc config.TaskConfig, changed map[string]struct{}) bool {
	for _, senderName := range tc.Senders {
		if _, ok := changed[senderName]; ok {
			return true
		}
	}
	return false
}

// expandTaskDeltaForPipelineChanges 把 pipeline 变化扩散到依赖它们的任务集合中。
//
// 即使任务本身配置未改，只要引用了发生变化的 pipeline，也必须被视为 remove+add。
func expandTaskDeltaForPipelineChanges(
	oldTasks map[string]config.TaskConfig,
	newTasks map[string]config.TaskConfig,
	taskAdded []string,
	taskRemoved []string,
	pipelineAdded []string,
	pipelineRemoved []string,
) ([]string, []string) {
	changedPipelines := make(map[string]struct{}, len(pipelineAdded)+len(pipelineRemoved))
	for _, name := range pipelineAdded {
		changedPipelines[name] = struct{}{}
	}
	for _, name := range pipelineRemoved {
		changedPipelines[name] = struct{}{}
	}
	if len(changedPipelines) == 0 {
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
		if !taskUsesChangedPipeline(oldTC, changedPipelines) && !taskUsesChangedPipeline(newTC, changedPipelines) {
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

// taskUsesChangedPipeline 判断任务是否依赖任一已变化的 pipeline。
func taskUsesChangedPipeline(tc config.TaskConfig, changed map[string]struct{}) bool {
	for _, pipelineName := range tc.Pipelines {
		if _, ok := changed[pipelineName]; ok {
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
			SelectorName:   rc.Selector,
		}
		st.mu.Lock()
		st.receivers[name] = rs
		st.mu.Unlock()
		if err := st.rebuildReceiverSelectors(); err != nil {
			return err
		}
		startAck := st.startReceiver(ctx, rs)
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
		go rs.Recv.Stop(ctx)
	}
	st.mu.Unlock()
	return nil
}

// startReceiver 异步启动单个 receiver。
//
// receiver 回调统一进入 dispatch，按当前 receiver 绑定的 selector 做精确匹配。
func (st *Store) startReceiver(ctx context.Context, rs *ReceiverState) <-chan struct{} {
	started := make(chan struct{})
	go func(state *ReceiverState, ack chan<- struct{}) {
		close(ack)
		err := state.Recv.Start(ctx, func(pkt *packet.Packet) { dispatchToSelector(ctx, state, pkt) })
		if err != nil {
			logx.L().Errorw("接收端异常退出", "组件名称", state.Name, "错误", err)
		}
	}(rs, started)
	return started
}

// waitReceiversStartInvoked 等待所有 receiver 的 Start 调用都已被 goroutine 触发。
//
// 它只保证 Start 已开始执行，不保证底层监听已经完全准备就绪。
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
//  4. 更新 task 索引与 stage 引用。
//
// counter 参数用于热重载：当旧 task 只是“同名重建”而非彻底删除时，
// runtime 会把旧 task 的聚合统计句柄传进来，让新 task 继续沿用累计值。
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
		ChannelQueueSize: tc.ChannelQueueSize,
		LogPayloadSend:   logOpts.send,
		PayloadLogMax:    logOpts.max,
	}
	if counter != nil {
		// 先把旧句柄挂到新 task，再 Start，这样 task.Start 就不会重复创建新的聚合统计对象。
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
	st.mu.Unlock()
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
		for _, sn := range ts.Cfg.Senders {
			if ss, ok := st.senders[sn]; ok {
				ss.Refs--
			}
		}
	}
	st.mu.Unlock()
	var counter *logx.TrafficCounter
	if ts != nil {
		if preserveCounter {
			// 先分离统计句柄，再 StopGraceful；这样 StopGraceful 不会关闭它。
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
		item := map[string]any{
			"task":            name,
			"pipelines":       ts.Cfg.Pipelines,
			"senders":         ts.Cfg.Senders,
			"execution_model": ts.T.ExecutionModel,
		}
		switch ts.T.ExecutionModel {
		case task.ExecutionModelPool:
			item["pool_size"] = ts.T.PoolSize
		case task.ExecutionModelChannel:
			item["channel_queue_size"] = ts.T.ChannelQueueSize
		}
		out = append(out, item)
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

// cloneSelectorConfigMap 深拷贝 selector 配置，避免热更新过程共享底层 map。
func cloneSelectorConfigMap(in map[string]config.SelectorConfig) map[string]config.SelectorConfig {
	out := make(map[string]config.SelectorConfig, len(in))
	for name, cfg := range in {
		cp := config.SelectorConfig{
			DefaultTaskSet: cfg.DefaultTaskSet,
		}
		if cfg.Matches != nil {
			cp.Matches = make(map[string]string, len(cfg.Matches))
			for key, taskSetName := range cfg.Matches {
				cp.Matches[key] = taskSetName
			}
		}
		out[name] = cp
	}
	return out
}

// cloneTaskSetMap 深拷贝 task_set 配置，避免热更新过程共享底层切片。
func cloneTaskSetMap(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for name, tasks := range in {
		out[name] = append([]string(nil), tasks...)
	}
	return out
}

// rebuildReceiverSelectors 根据当前 selectors、taskSets 与 tasks 重新编译每个 receiver 的只读 selector 快照。
//
// 该函数会先在锁外准备编译输入，再逐个原子替换 receiver 内的 selector，减少热路径读锁竞争。
func (st *Store) rebuildReceiverSelectors() error {
	st.mu.RLock()
	selectorCfg := cloneSelectorConfigMap(st.selectors)
	taskSets := cloneTaskSetMap(st.taskSets)
	tasksByName := make(map[string]*TaskState, len(st.tasks))
	for name, ts := range st.tasks {
		tasksByName[name] = ts
	}
	receivers := make(map[string]*ReceiverState, len(st.receivers))
	for name, rs := range st.receivers {
		receivers[name] = rs
	}
	st.mu.RUnlock()

	compiled := make(map[string]*CompiledSelector, len(selectorCfg))
	for name, sc := range selectorCfg {
		cs := newCompiledSelector(name, len(sc.Matches))
		for key, taskSetName := range sc.Matches {
			taskStates, err := resolveTaskSetStates(name, taskSetName, taskSets, tasksByName)
			if err != nil {
				return err
			}
			cs.TasksByKey[key] = taskStates
		}
		if sc.DefaultTaskSet != "" {
			defaultTasks, err := resolveTaskSetStates(name, sc.DefaultTaskSet, taskSets, tasksByName)
			if err != nil {
				return err
			}
			cs.DefaultTasks = defaultTasks
		}
		compiled[name] = cs
	}

	for _, rs := range receivers {
		rs.Selector.Store(compiled[rs.SelectorName])
	}
	return nil
}

// resolveTaskSetStates 将 task_set 名称展开为运行时任务状态切片。
//
// 该函数统一处理 selector 的显式匹配项和默认任务集，避免同一段“task_set -> taskState”展开逻辑在编译阶段重复维护。
func resolveTaskSetStates(selectorName, taskSetName string, taskSets map[string][]string, tasksByName map[string]*TaskState) ([]*TaskState, error) {
	taskNames := taskSets[taskSetName]
	taskStates := make([]*TaskState, 0, len(taskNames))
	for _, taskName := range taskNames {
		ts := tasksByName[taskName]
		if ts == nil {
			return nil, fmt.Errorf("selector %s task %s not found during compile", selectorName, taskName)
		}
		taskStates = append(taskStates, ts)
	}
	return taskStates, nil
}

// dispatchToSelector 将单个输入包 fan-out 到 receiver 当前 selector 命中的所有任务。
//
// 热路径仅执行：
//  1. receiver 已生成好的 match key；
//  2. selector 对该 key 做一次精确 map lookup；
//  3. 将命中的 task slice fan-out。
//
// 与聚合统计日志的关系：
//   - receiver 在进入这里之前通常已调用 TrafficCounter.AddBytes；
//   - task 在 Handle->sendToSender 过程中再追加 task send traffic stats；
//   - 因此这里是“接收统计”和“发送统计”之间的中间分发桥梁。
func dispatchToSelector(ctx context.Context, rs *ReceiverState, pkt *packet.Packet) {
	if rs == nil {
		pkt.Release()
		return
	}
	selectorAny := rs.Selector.Load()
	var selector *CompiledSelector
	if selectorAny != nil {
		selector = selectorAny.(*CompiledSelector)
	}
	if rs.LogPayloadRecv {
		// payload 摘要是纯观测行为，不影响 selector 匹配结果。
		logReceiverPayload(rs.Name, pkt, rs.PayloadLogMax)
	}
	tasks := selector.Match(pkt.Meta.MatchKey)
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

// dispatch 为兼容旧测试入口保留的分发包装函数，按 receiver 名称查询状态后转给 dispatchToSelector。
func dispatch(ctx context.Context, st *Store, receiverName string, pkt *packet.Packet) {
	st.mu.RLock()
	rs := st.receivers[receiverName]
	st.mu.RUnlock()
	dispatchToSelector(ctx, rs, pkt)
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
	logx.L().Infow("接收端接收Payload摘要",
		"组件名称", receiver,
		"载荷类型", pkt.Kind,
		"Payload长度", len(pkt.Payload),
		"Payload十六进制", payloadHex(pkt.Payload, maxBytes),
		"传输ID", pkt.Meta.TransferID,
		"偏移量", pkt.Meta.Offset,
		"总大小", pkt.Meta.TotalSize,
		"是否EOF", pkt.Meta.EOF,
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
		return hex.EncodeToString(b[:max]) + "...(已截断)"
	}
	return hex.EncodeToString(b)
}

// buildReceiver 根据 receiver 配置创建具体接收端实例。
//
// 该函数只做实例构造，不负责启动；调用方应在成功后自行接入 Store 与生命周期管理。
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
		return receiver.NewGnetUDP(name, rc.Listen, multicore, numEventLoop, rc.ReadBufferCap, rc.SocketRecvBuffer, gnetLogLevel, rc.MatchKey)
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
		return receiver.NewGnetTCP(name, rc.Listen, multicore, numEventLoop, rc.ReadBufferCap, rc.SocketRecvBuffer, fr, gnetLogLevel, rc.MatchKey)
	case "kafka":
		return receiver.NewKafkaReceiver(name, rc)
	case "sftp":
		return receiver.NewSFTPReceiver(name, rc)
	default:
		return nil, fmt.Errorf("receiver %s unknown type %s", name, rc.Type)
	}
}

// buildSender 根据 sender 配置创建具体发送端实例。
//
// 该函数只做实例构造，不负责引用计数或关闭旧实例；这些工作由调用方在更新流程中处理。
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
		return sender.NewUDPUnicastSender(name, sc.LocalIP, sc.LocalPort, sc.Remote, sc.SocketSendBuffer, conc)
	case "udp_multicast":
		if sc.LocalPort <= 0 {
			return nil, fmt.Errorf("sender %s udp_multicast requires local_port", name)
		}
		return sender.NewUDPMulticastSender(name, sc.LocalIP, sc.LocalPort, sc.Remote, sc.Iface, sc.TTL, sc.Loop, sc.SocketSendBuffer, conc)
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
		return sender.NewGnetTCPSender(name, sc.Remote, with, conc, sc.SocketSendBuffer, gnetLogLevel)
	case "kafka":
		return sender.NewKafkaSender(name, sc)
	case "sftp":
		return sender.NewSFTPSender(name, sc)
	default:
		return nil, fmt.Errorf("sender %s unknown type %s", name, sc.Type)
	}
}
