// update_cache.go 实现运行时缓存的全量替换流程。
package runtime

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
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
	recv bool
	send bool
	max  int
}

func buildTaskPayloadLogOptions(name string, tc config.TaskConfig, lc config.LoggingConfig) taskPayloadLogOptions {
	if len(lc.PayloadLogTasks) == 0 {
		return taskPayloadLogOptions{max: lc.PayloadLogMaxBytes}
	}
	enabled := false
	for _, n := range lc.PayloadLogTasks {
		if strings.TrimSpace(n) == name {
			enabled = true
			break
		}
	}
	if !enabled {
		return taskPayloadLogOptions{max: lc.PayloadLogMaxBytes}
	}
	return taskPayloadLogOptions{
		recv: lc.PayloadLogRecv && tc.LogPayloadRecv,
		send: lc.PayloadLogSend && tc.LogPayloadSend,
		max:  lc.PayloadLogMaxBytes,
	}
}

func UpdateCache(ctx context.Context, st *Store, cfg config.Config) error {
	lg := logx.L()
	start := time.Now()
	if logx.Enabled(zapcore.InfoLevel) {
		lg.Infow("updating runtime cache", "version", cfg.Version, "receivers", len(cfg.Receivers), "tasks", len(cfg.Tasks), "senders", len(cfg.Senders))
	}

	st.tryRestartStoppedReceiversOnce(ctx, cfg.Receivers, cfg.Logging.Level)

	if st.canApplyBusinessDelta(cfg) {
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

	// 给 gnet 一个很短的启动时间，避免立即 Stop/Update 时边界问题。
	time.Sleep(10 * time.Millisecond)
	if logx.Enabled(zapcore.InfoLevel) {
		lg.Infow("runtime cache updated", "version", cfg.Version, "cost", time.Since(start))
	}
	return nil
}

func (st *Store) canApplyBusinessDelta(cfg config.Config) bool {
	st.mu.RLock()
	defer st.mu.RUnlock()
	if len(st.receivers) == 0 && len(st.senders) == 0 && len(st.tasks) == 0 {
		return false
	}
	for _, rs := range st.receivers {
		if !rs.Running {
			return false
		}
	}
	return true
}

func (st *Store) replaceAll(ctx context.Context, cfg config.Config) error {
	_ = st.StopAll(ctx)

	compiled, err := CompilePipelines(cfg.Pipelines)
	if err != nil {
		return err
	}

	// 一次性替换 store 内部索引，确保新配置以完整快照生效。
	st.mu.Lock()
	for name := range st.receivers {
		logx.UnregisterReceiverRuntimeStats(name)
	}
	st.receivers = make(map[string]*ReceiverState)
	st.senders = make(map[string]*SenderState)
	st.tasks = make(map[string]*TaskState)
	st.pipelines = compiled
	st.pipelineCfg = cfg.Pipelines
	st.subs = make(map[string]map[string]struct{})
	st.version = cfg.Version
	st.mu.Unlock()
	st.setDispatchSubs(map[string][]*TaskState{})

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

		logOpts := buildTaskPayloadLogOptions(name, tc, cfg.Logging)
		tk := &task.Task{
			Name:             name,
			Pipelines:        pipes,
			Senders:          sends,
			PoolSize:         tc.PoolSize,
			FastPath:         tc.FastPath,
			ExecutionModel:   tc.ExecutionModel,
			QueueSize:        tc.QueueSize,
			ChannelQueueSize: tc.ChannelQueueSize,
			LogPayloadRecv:   logOpts.recv,
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
	for name, rc := range cfg.Receivers {
		r, err := buildReceiver(name, rc, cfg.Logging.Level)
		if err != nil {
			return err
		}
		rs := &ReceiverState{Name: name, Cfg: rc, Recv: r}
		st.mu.Lock()
		st.receivers[name] = rs
		if _, ok := st.subs[name]; !ok {
			st.subs[name] = make(map[string]struct{})
		}
		st.mu.Unlock()
		st.registerReceiverRuntimeStats(name)
		st.startReceiver(ctx, name, r)
	}

	return nil
}

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

	receiverChanged := len(receiverAdded) > 0 || len(receiverRemoved) > 0
	senderChanged := len(senderAdded) > 0 || len(senderRemoved) > 0
	pipelineChanged := len(pipelineAdded) > 0 || len(pipelineRemoved) > 0

	if pipelineChanged {
		compiled, err := CompilePipelines(cfg.Pipelines)
		if err != nil {
			return err
		}
		st.mu.Lock()
		st.pipelines = compiled
		st.pipelineCfg = cfg.Pipelines
		st.mu.Unlock()
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
	st.gcUnusedSenders()
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
		rs := &ReceiverState{Name: name, Cfg: rc, Recv: r}
		st.mu.Lock()
		st.receivers[name] = rs
		if _, ok := st.subs[name]; !ok {
			st.subs[name] = make(map[string]struct{})
		}
		st.mu.Unlock()
		st.registerReceiverRuntimeStats(name)
		st.startReceiver(ctx, name, r)
		if ok {
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
		logx.UnregisterReceiverRuntimeStats(name)
		go rs.Recv.Stop(ctx)
	}
	st.mu.Unlock()
	return nil
}

func (st *Store) tryRestartStoppedReceiversOnce(ctx context.Context, next map[string]config.ReceiverConfig, gnetLogLevel string) {
	type restartCandidate struct {
		name string
		cfg  config.ReceiverConfig
	}

	candidates := make([]restartCandidate, 0)
	st.mu.RLock()
	for name, rs := range st.receivers {
		if rs.Running || rs.RestartAttempted {
			continue
		}
		rc, ok := next[name]
		if !ok || !reflect.DeepEqual(rs.Cfg, rc) {
			continue
		}
		candidates = append(candidates, restartCandidate{name: name, cfg: rc})
	}
	st.mu.RUnlock()

	for _, c := range candidates {
		st.mu.Lock()
		rs := st.receivers[c.name]
		if rs == nil || rs.Running || rs.RestartAttempted || !reflect.DeepEqual(rs.Cfg, c.cfg) {
			st.mu.Unlock()
			continue
		}
		rs.RestartAttempted = true
		st.mu.Unlock()

		r, err := buildReceiver(c.name, c.cfg, gnetLogLevel)
		if err != nil {
			st.mu.Lock()
			if rs := st.receivers[c.name]; rs != nil {
				rs.LastStartError = err.Error()
			}
			st.mu.Unlock()
			logx.L().Errorw("receiver restart build failed", "receiver", c.name, "error", err)
			continue
		}

		var old receiver.Receiver
		st.mu.Lock()
		if rs := st.receivers[c.name]; rs != nil {
			old = rs.Recv
			rs.Recv = r
			rs.Cfg = c.cfg
			rs.LastStartError = ""
		}
		st.mu.Unlock()

		if old != nil {
			_ = old.Stop(ctx)
		}
		st.registerReceiverRuntimeStats(c.name)
		st.startReceiver(ctx, c.name, r)
	}
}

func (st *Store) registerReceiverRuntimeStats(name string) {
	logx.RegisterReceiverRuntimeStats(name, func() logx.ReceiverRuntimeStats {
		st.mu.RLock()
		defer st.mu.RUnlock()
		rs := st.receivers[name]
		if rs == nil {
			return logx.ReceiverRuntimeStats{}
		}
		return logx.ReceiverRuntimeStats{Running: rs.Running, RestartAttempted: rs.RestartAttempted, LastStartError: rs.LastStartError}
	})
}

func (st *Store) startReceiver(ctx context.Context, name string, r receiver.Receiver) {
	st.mu.Lock()
	if rs := st.receivers[name]; rs != nil {
		rs.Running = true
		rs.LastStartError = ""
	}
	st.mu.Unlock()

	go func(r receiver.Receiver, rn string) {
		err := r.Start(ctx, func(pkt *packet.Packet) { dispatch(ctx, st, rn, pkt) })
		st.mu.Lock()
		if rs := st.receivers[rn]; rs != nil && rs.Recv == r {
			rs.Running = false
			if err != nil {
				rs.LastStartError = err.Error()
			}
		}
		st.mu.Unlock()
		if err != nil {
			logx.L().Errorw("receiver stopped with error", "receiver", rn, "error", err)
		}
	}(r, name)
}

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

	logOpts := buildTaskPayloadLogOptions(name, tc, lc)
	tk := &task.Task{
		Name:             name,
		Pipelines:        pipes,
		Senders:          sends,
		PoolSize:         tc.PoolSize,
		FastPath:         tc.FastPath,
		ExecutionModel:   tc.ExecutionModel,
		QueueSize:        tc.QueueSize,
		ChannelQueueSize: tc.ChannelQueueSize,
		LogPayloadRecv:   logOpts.recv,
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

func (st *Store) removeTask(name string, preserveCounter bool) *logx.TrafficCounter {
	st.mu.Lock()
	ts := st.tasks[name]
	if ts != nil {
		delete(st.tasks, name)
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

func receiverConfigSnapshot(m map[string]*ReceiverState) map[string]config.ReceiverConfig {
	out := make(map[string]config.ReceiverConfig, len(m))
	for n, s := range m {
		out[n] = s.Cfg
	}
	return out
}

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

	if len(tasks) == 0 {
		pkt.Release()
		return
	}

	// 仅有一个订阅任务时直接复用原始包，避免无意义二次复制。
	if len(tasks) == 1 {
		tasks[0].T.LogPayloadReceive(receiverName, pkt)
		tasks[0].T.Handle(ctx, pkt)
		return
	}

	// 多任务场景下，为避免共享 packet 带来的释放时序竞争，
	// 先为其余任务逐个 Clone，再把原始包交给首个任务，避免额外中间切片。
	for _, ts := range tasks[1:] {
		cp := pkt.Clone()
		ts.T.LogPayloadReceive(receiverName, cp)
		ts.T.Handle(ctx, cp)
	}
	first := tasks[0]
	first.T.LogPayloadReceive(receiverName, pkt)
	first.T.Handle(ctx, pkt)
}

// buildReceiver 负责该函数对应的核心逻辑，详见实现细节。
func buildReceiver(name string, rc config.ReceiverConfig, gnetLogLevel string) (receiver.Receiver, error) {
	multicore := rc.Multicore
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
