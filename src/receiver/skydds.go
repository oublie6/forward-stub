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
	name    string
	cfg     config.ReceiverConfig
	reader  skydds.Reader
	builder func() string
	mode    string
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
	return &SkyDDSReceiver{name: name, cfg: rc, reader: r, builder: builder, mode: mode}, nil
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
		payload, err := r.reader.Poll(500 * time.Millisecond)
		if err != nil {
			return fmt.Errorf("skydds poll: %w", err)
		}
		if len(payload) == 0 {
			continue
		}
		buf, rel := packet.CopyFrom(payload)
		onPacket(&packet.Packet{Envelope: packet.Envelope{Kind: packet.PayloadKindStream, Payload: buf, Meta: packet.Meta{
			Proto: packet.ProtoSkyDDS, Remote: r.cfg.TopicName, Local: fmt.Sprintf("domain:%d", r.cfg.DomainID), MatchKey: r.builder(),
		}}, ReleaseFn: rel})
	}
}

func (r *SkyDDSReceiver) Stop(ctx context.Context) error {
	_ = ctx
	if err := r.reader.Close(); err != nil {
		logx.L().Warnw("SkyDDS receiver close failed", "receiver", r.name, "error", err)
		return err
	}
	return nil
}
