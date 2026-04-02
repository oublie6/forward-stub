package receiver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/logx"
	"forward-stub/src/packet"
	"forward-stub/src/skydds"
)

type SkyDDSReceiver struct {
	name          string
	cfg           config.ReceiverConfig
	reader        skydds.Reader
	builder       func() string
	mode          string
	model         string
	waitTimeout   time.Duration
	drainMaxItems int
}

func NewSkyDDSReceiver(name string, rc config.ReceiverConfig) (*SkyDDSReceiver, error) {
	r, err := skydds.NewReader(skydds.CommonOptions{
		DCPSConfigFile: rc.DCPSConfigFile,
		DomainID:       rc.DomainID,
		TopicName:      rc.TopicName,
		MessageModel:   strings.ToLower(strings.TrimSpace(rc.MessageModel)),
	})
	if err != nil {
		return nil, fmt.Errorf("new skydds reader: %w", err)
	}
	builder := func() string { return "skydds|topic_name=" + rc.TopicName }
	mode := receiverMatchKeyModeCompatDefault
	if strings.TrimSpace(rc.MatchKey.Mode) == "fixed" {
		fixed := buildSingleFieldMatchKey("skydds", "fixed", rc.MatchKey.FixedValue)
		builder = func() string { return fixed }
		mode = "fixed"
	}
	waitTimeout, err := time.ParseDuration(rc.WaitTimeout)
	if err != nil || waitTimeout <= 0 {
		return nil, fmt.Errorf("invalid skydds receiver wait_timeout: %q", rc.WaitTimeout)
	}
	if rc.DrainMaxItems <= 0 {
		return nil, fmt.Errorf("invalid skydds receiver drain_max_items: %d", rc.DrainMaxItems)
	}
	return &SkyDDSReceiver{
		name:          name,
		cfg:           rc,
		reader:        r,
		builder:       builder,
		mode:          mode,
		model:         strings.ToLower(strings.TrimSpace(rc.MessageModel)),
		waitTimeout:   waitTimeout,
		drainMaxItems: rc.DrainMaxItems,
	}, nil
}

func (r *SkyDDSReceiver) Name() string { return r.name }
func (r *SkyDDSReceiver) Key() string {
	return fmt.Sprintf("dds_skydds|domain=%d|topic=%s", r.cfg.DomainID, r.cfg.TopicName)
}
func (r *SkyDDSReceiver) MatchKeyMode() string { return r.mode }

func (r *SkyDDSReceiver) Start(ctx context.Context, onPacket func(*packet.Packet)) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		ready, err := r.reader.Wait(r.waitTimeout)
		if err != nil {
			return fmt.Errorf("skydds receiver wait: %w", err)
		}
		if !ready {
			continue
		}

		for {
			payloads, err := r.reader.Drain(r.drainMaxItems)
			if err != nil {
				return fmt.Errorf("skydds receiver drain: %w", err)
			}
			if len(payloads) == 0 {
				break
			}
			for i := range payloads {
				if len(payloads[i]) == 0 {
					logx.L().Warnw("SkyDDS sub-message empty; skip", "receiver", r.name, "index", i, "message_model", r.model)
					continue
				}
				// 注意：即使是批量拉取到的一组消息，也必须逐条形成独立 packet 并下发。
				r.emitPacket(payloads[i], onPacket)
			}
		}
	}
}

func (r *SkyDDSReceiver) emitPacket(payload []byte, onPacket func(*packet.Packet)) {
	buf, rel := packet.CopyFrom(payload)
	onPacket(&packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindStream, Payload: buf, Meta: packet.Meta{
		Proto: packet.ProtoSkyDDS, Remote: r.cfg.TopicName, Local: fmt.Sprintf("domain:%d", r.cfg.DomainID), MatchKey: r.builder(),
	}}, ReleaseFn: rel})
}

func (r *SkyDDSReceiver) Stop(ctx context.Context) error {
	_ = ctx
	if err := r.reader.Close(); err != nil {
		logx.L().Warnw("SkyDDS receiver close failed", "receiver", r.name, "error", err)
		return err
	}
	return nil
}
