package runtime

import (
	"context"
	"fmt"
	"time"

	"forword-stub/src/config"
	"forword-stub/src/logx"
	"forword-stub/src/packet"
	"forword-stub/src/pipeline"
	"forword-stub/src/receiver"
	"forword-stub/src/sender"
	"forword-stub/src/task"

	"go.uber.org/zap/zapcore"
)

// UpdateCache: 简化实现 —— 先整体停掉旧对象，再按新配置重建并启动。
// 这样代码更短、更稳（便于离线环境快速落地）；后续如果需要“热更新差异化”再扩展。
func UpdateCache(ctx context.Context, st *Store, cfg config.Config) error {
	lg := logx.L()
	start := time.Now()
	if logx.Enabled(zapcore.InfoLevel) {
		lg.Infow("updating runtime cache", "version", cfg.Version, "receivers", len(cfg.Receivers), "tasks", len(cfg.Tasks), "senders", len(cfg.Senders))
	}

	_ = st.StopAll(ctx)

	compiled, err := CompilePipelines(cfg.Pipelines)
	if err != nil {
		return err
	}

	st.mu.Lock()
	st.receivers = make(map[string]*ReceiverState)
	st.senders = make(map[string]*SenderState)
	st.tasks = make(map[string]*TaskState)
	st.pipelines = compiled
	st.subs = make(map[string]map[string]struct{})
	st.version = cfg.Version
	st.mu.Unlock()

	// build senders
	for name, sc := range cfg.Senders {
		s, err := buildSender(name, sc, cfg.Logging.Level)
		if err != nil {
			return err
		}
		st.mu.Lock()
		st.senders[name] = &SenderState{Name: name, Cfg: sc, S: s, Refs: 0}
		st.mu.Unlock()
	}

	// build tasks
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

		tk := &task.Task{
			Name:      name,
			Pipelines: pipes,
			Senders:   sends,
			PoolSize:  tc.PoolSize,
			FastPath:  tc.FastPath,
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

	// build & start receivers
	for name, rc := range cfg.Receivers {
		r, err := buildReceiver(name, rc, cfg.Logging.Level)
		if err != nil {
			return err
		}
		rs := &ReceiverState{Name: name, Cfg: rc, Recv: r, Running: true}
		st.mu.Lock()
		st.receivers[name] = rs
		if _, ok := st.subs[name]; !ok {
			st.subs[name] = make(map[string]struct{})
		}
		st.mu.Unlock()

		go func(r receiver.Receiver, rn string) {
			if err := r.Start(ctx, func(pkt *packet.Packet) { dispatch(ctx, st, rn, pkt) }); err != nil && logx.Enabled(zapcore.ErrorLevel) {
				lg.Errorw("receiver stopped with error", "receiver", rn, "error", err)
			}
		}(r, name)
	}

	// 给 gnet 一个很短的启动时间，避免立即 Stop/Update 时边界问题
	time.Sleep(10 * time.Millisecond)
	if logx.Enabled(zapcore.InfoLevel) {
		lg.Infow("runtime cache updated", "version", cfg.Version, "cost", time.Since(start))
	}
	return nil
}

func dispatch(ctx context.Context, st *Store, receiverName string, pkt *packet.Packet) {
	st.mu.Lock()
	sub := st.subs[receiverName]
	tasks := make([]*TaskState, 0, len(sub))
	for tn := range sub {
		if ts := st.tasks[tn]; ts != nil {
			tasks = append(tasks, ts)
		}
	}
	st.mu.Unlock()

	if len(tasks) == 0 {
		pkt.Release()
		return
	}

	for i, ts := range tasks {
		sendPkt := pkt
		if i > 0 {
			sendPkt = pkt.Clone()
		}
		ts.T.Handle(ctx, sendPkt)
	}
}

func buildReceiver(name string, rc config.ReceiverConfig, gnetLogLevel string) (receiver.Receiver, error) {
	switch rc.Type {
	case "udp_gnet":
		return receiver.NewGnetUDP(name, rc.Listen, rc.Multicore, gnetLogLevel), nil
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
		return receiver.NewGnetTCP(name, rc.Listen, rc.Multicore, fr, gnetLogLevel), nil
	default:
		return nil, fmt.Errorf("receiver %s unknown type %s", name, rc.Type)
	}
}

func buildSender(name string, sc config.SenderConfig, gnetLogLevel string) (sender.Sender, error) {
	conc := sc.Concurrency
	if conc <= 0 {
		conc = 1
	}
	switch sc.Type {
	case "udp_unicast":
		if sc.LocalPort <= 0 {
			return nil, fmt.Errorf("sender %s udp_unicast requires local_port", name)
		}
		_ = conc
		return sender.NewUDPUnicastSender(name, sc.LocalIP, sc.LocalPort, sc.Remote)
	case "udp_multicast":
		if sc.LocalPort <= 0 {
			return nil, fmt.Errorf("sender %s udp_multicast requires local_port", name)
		}
		_ = conc
		return sender.NewUDPMulticastSender(name, sc.LocalIP, sc.LocalPort, sc.Remote, sc.Iface, sc.TTL, sc.Loop)
	case "tcp_gnet":
		with := sc.Frame == "u16be"
		return sender.NewGnetTCPSender(name, sc.Remote, with, conc, gnetLogLevel)
	case "kafka":
		return sender.NewKafkaSender(name, sc.Topic), nil
	default:
		return nil, fmt.Errorf("sender %s unknown type %s", name, sc.Type)
	}
}
