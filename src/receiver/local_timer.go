// local_timer.go 实现本地定时固定报文生成 receiver。
package receiver

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/logx"
	"forward-stub/src/packet"

	"go.uber.org/zap/zapcore"
)

// LocalTimerReceiver 按配置在本地周期性生成固定 payload。
//
// 该 receiver 不接入外部网络或文件系统，只负责把本地造出的 packet
// 交给 runtime 注入的 onPacket，后续仍走 selector -> task -> sender 主链路。
type LocalTimerReceiver struct {
	name string
	cfg  config.ReceiverConfig

	payload []byte

	matchKeyBuilder localTimerMatchKeyBuilder
	matchKeyMode    string

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}

	stats *logx.TrafficCounter
}

// NewLocalTimerReceiver 构造本地定时造数 receiver，并预解析 payload 与 match key。
func NewLocalTimerReceiver(name string, rc config.ReceiverConfig) (*LocalTimerReceiver, error) {
	payload, err := decodeLocalTimerPayload(rc.Generator)
	if err != nil {
		return nil, err
	}
	builder, mode, err := compileLocalTimerMatchKeyBuilder(rc.MatchKey)
	if err != nil {
		return nil, err
	}
	return &LocalTimerReceiver{
		name:            name,
		cfg:             rc,
		payload:         payload,
		matchKeyBuilder: builder,
		matchKeyMode:    mode,
	}, nil
}

// Name 返回 receiver 配置名。
func (r *LocalTimerReceiver) Name() string { return r.name }

// Key 返回 receiver 稳定身份键，用于热更新比较与观测。
func (r *LocalTimerReceiver) Key() string {
	g := r.cfg.Generator
	return "local_timer|" + strings.TrimSpace(g.Interval) + "|" + fmt.Sprint(g.RatePerSec) + "|" + strings.TrimSpace(g.TickInterval) + "|" + fmt.Sprint(g.TotalPackets)
}

// MatchKeyMode 返回 local_timer receiver 当前生效的 match key 模式。
func (r *LocalTimerReceiver) MatchKeyMode() string { return r.matchKeyMode }

// Start 启动本地定时造数循环。
// 用法：Start 会阻塞直到 ctx 取消、Stop 被调用或 total_packets 达到上限。
func (r *LocalTimerReceiver) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
	rctx, cancel := context.WithCancel(ctx)

	r.mu.Lock()
	r.cancel = cancel
	r.done = make(chan struct{})
	if logx.Enabled(zapcore.InfoLevel) {
		r.stats = logx.AcquireTrafficCounter(
			"receiver traffic stats",
			"role", "receiver",
			"receiver", r.Name(),
			"receiver_key", r.Key(),
			"proto", "local",
		)
	}
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		if r.stats != nil {
			r.stats.Close()
			r.stats = nil
		}
		if r.cancel != nil {
			r.cancel()
			r.cancel = nil
		}
		if r.done != nil {
			close(r.done)
			r.done = nil
		}
		r.mu.Unlock()
	}()

	if d, ok, err := parseOptionalDuration(r.cfg.Generator.StartDelay); err != nil {
		return err
	} else if ok {
		timer := time.NewTimer(d)
		select {
		case <-rctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}

	if strings.TrimSpace(r.cfg.Generator.Interval) != "" {
		return r.runInterval(rctx, onPacket)
	}
	return r.runRate(rctx, onPacket)
}

// Stop 请求停止并等待 Start 退出。
func (r *LocalTimerReceiver) Stop(ctx context.Context) error {
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *LocalTimerReceiver) runInterval(ctx context.Context, onPacket func(*packet.Packet)) error {
	interval, err := time.ParseDuration(strings.TrimSpace(r.cfg.Generator.Interval))
	if err != nil || interval <= 0 {
		return fmt.Errorf("local_timer receiver interval must be a valid duration > 0")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var sent int64
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if r.emit(onPacket) {
				sent++
			}
			if r.reachedTotal(sent) {
				return nil
			}
		}
	}
}

func (r *LocalTimerReceiver) runRate(ctx context.Context, onPacket func(*packet.Packet)) error {
	tick := config.DefaultLocalTimerTickInterval
	if strings.TrimSpace(r.cfg.Generator.TickInterval) != "" {
		tick = r.cfg.Generator.TickInterval
	}
	tickDuration, err := time.ParseDuration(strings.TrimSpace(tick))
	if err != nil || tickDuration <= 0 {
		return fmt.Errorf("local_timer receiver tick_interval must be a valid duration > 0")
	}
	rate := r.cfg.Generator.RatePerSec
	if rate <= 0 {
		return fmt.Errorf("local_timer receiver rate_per_sec must be > 0")
	}

	ticker := time.NewTicker(tickDuration)
	defer ticker.Stop()

	perTick := float64(rate) * tickDuration.Seconds()
	credit := 0.0
	var sent int64
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			credit += perTick
			for credit >= 1 {
				if r.emit(onPacket) {
					sent++
				}
				credit -= 1
				if r.reachedTotal(sent) {
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				default:
				}
			}
		}
	}
}

func (r *LocalTimerReceiver) emit(onPacket func(*packet.Packet)) bool {
	payload, rel := packet.CopyFrom(r.payload)
	if r.stats != nil {
		r.stats.AddBytes(len(payload))
	}
	g := r.cfg.Generator
	onPacket(&packet.Packet{
		Envelope: packet.Envelope{
			Kind:    packet.PayloadKindStream,
			Payload: payload,
			Meta: packet.Meta{
				Proto:        packet.ProtoLocal,
				ReceiverName: r.name,
				Remote:       g.Remote,
				Local:        g.Local,
				MatchKey:     r.matchKeyBuilder(),
			},
		},
		ReleaseFn: rel,
	})
	return true
}

func (r *LocalTimerReceiver) reachedTotal(sent int64) bool {
	total := r.cfg.Generator.TotalPackets
	return total > 0 && sent >= total
}

func decodeLocalTimerPayload(g config.LocalGeneratorConfig) ([]byte, error) {
	data := strings.TrimSpace(g.PayloadData)
	switch strings.TrimSpace(g.PayloadFormat) {
	case "hex":
		b, err := hex.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("local_timer receiver payload_data hex invalid: %w", err)
		}
		return b, nil
	case "text":
		return []byte(g.PayloadData), nil
	case "base64":
		b, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("local_timer receiver payload_data base64 invalid: %w", err)
		}
		return b, nil
	default:
		return nil, fmt.Errorf("local_timer receiver payload_format must be one of hex/text/base64")
	}
}

func parseOptionalDuration(value string) (time.Duration, bool, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return 0, false, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, false, fmt.Errorf("local_timer receiver start_delay must be a valid duration > 0")
	}
	return d, true, nil
}
