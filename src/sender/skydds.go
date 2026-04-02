package sender

import (
	"context"
	"fmt"
	"strings"

	"forward-stub/src/config"
	"forward-stub/src/logx"
	"forward-stub/src/packet"
	"forward-stub/src/skydds"
)

type SkyDDSSender struct {
	name   string
	cfg    config.SenderConfig
	writer skydds.Writer
}

func NewSkyDDSSender(name string, sc config.SenderConfig) (*SkyDDSSender, error) {
	w, err := skydds.NewWriter(skydds.CommonOptions{
		DCPSConfigFile: sc.DCPSConfigFile,
		DomainID:       sc.DomainID,
		TopicName:      sc.TopicName,
		MessageModel:   strings.ToLower(strings.TrimSpace(sc.MessageModel)),
	})
	if err != nil {
		return nil, fmt.Errorf("new skydds writer: %w", err)
	}
	return &SkyDDSSender{name: name, cfg: sc, writer: w}, nil
}

func (s *SkyDDSSender) Name() string { return s.name }
func (s *SkyDDSSender) Key() string {
	return fmt.Sprintf("dds_skydds|domain=%d|topic=%s", s.cfg.DomainID, s.cfg.TopicName)
}
func (s *SkyDDSSender) Send(ctx context.Context, p *packet.Packet) error {
	_ = ctx
	if err := s.writer.Write(p.Payload); err != nil {
		logx.L().Warnw("SkyDDS send failed", "sender", s.name, "error", err)
		return err
	}
	return nil
}
func (s *SkyDDSSender) Close(ctx context.Context) error {
	_ = ctx
	return s.writer.Close()
}
