package sender

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/logx"
	"forward-stub/src/packet"
	"forward-stub/src/skydds"
)

var skyddsWriterFactory = skydds.NewWriter

type SkyDDSSender struct {
	name   string
	cfg    config.SenderConfig
	writer skydds.Writer
	model  string

	mu         sync.Mutex
	closed     bool
	batch      [][]byte
	batchBytes int
	batchNum   int
	batchSize  int
	batchDelay time.Duration
	flushTimer *time.Timer
}

func NewSkyDDSSender(name string, sc config.SenderConfig) (*SkyDDSSender, error) {
	w, err := skyddsWriterFactory(skydds.CommonOptions{
		DCPSConfigFile: sc.DCPSConfigFile,
		DomainID:       sc.DomainID,
		TopicName:      sc.TopicName,
		MessageModel:   strings.ToLower(strings.TrimSpace(sc.MessageModel)),
	})
	if err != nil {
		return nil, fmt.Errorf("new skydds writer: %w", err)
	}
	return newSkyDDSSenderWithWriter(name, sc, w)
}

func newSkyDDSSenderWithWriter(name string, sc config.SenderConfig, w skydds.Writer) (*SkyDDSSender, error) {
	model := strings.ToLower(strings.TrimSpace(sc.MessageModel))
	s := &SkyDDSSender{name: name, cfg: sc, writer: w, model: model}
	if model == "batch_octet" {
		d, err := time.ParseDuration(sc.BatchDelay)
		if err != nil {
			return nil, fmt.Errorf("invalid skydds batch_delay: %w", err)
		}
		s.batchNum = sc.BatchNum
		s.batchSize = sc.BatchSize
		s.batchDelay = d
	}
	return s, nil
}

func (s *SkyDDSSender) Name() string { return s.name }
func (s *SkyDDSSender) Key() string {
	return fmt.Sprintf("dds_skydds|domain=%d|topic=%s", s.cfg.DomainID, s.cfg.TopicName)
}

func (s *SkyDDSSender) Send(ctx context.Context, p *packet.Packet) error {
	_ = ctx
	if s.model != "batch_octet" {
		if err := s.writer.Write(p.Payload); err != nil {
			logx.L().Warnw("SkyDDS send failed", "sender", s.name, "error", err)
			return err
		}
		return nil
	}
	return s.sendBatch(p.Payload)
}

func (s *SkyDDSSender) sendBatch(payload []byte) error {
	msg := append([]byte(nil), payload...)

	var flushNow [][]byte
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("skydds sender closed")
	}

	if len(msg) > s.batchSize && len(s.batch) == 0 {
		// 单条消息超过 batch_size：直接单条成批发送。
		flushNow = [][]byte{msg}
		s.mu.Unlock()
		return s.flushPayloads(flushNow, "oversize_single")
	}

	s.batch = append(s.batch, msg)
	s.batchBytes += len(msg)
	if len(s.batch) == 1 {
		s.startFlushTimerLocked()
	}

	if len(s.batch) >= s.batchNum || s.batchBytes >= s.batchSize {
		flushNow = s.drainBatchLocked()
		s.stopFlushTimerLocked()
	}
	s.mu.Unlock()

	if len(flushNow) > 0 {
		return s.flushPayloads(flushNow, "threshold")
	}
	return nil
}

func (s *SkyDDSSender) startFlushTimerLocked() {
	if s.flushTimer != nil {
		s.flushTimer.Stop()
	}
	s.flushTimer = time.AfterFunc(s.batchDelay, func() {
		_ = s.flushByTimer()
	})
}

func (s *SkyDDSSender) stopFlushTimerLocked() {
	if s.flushTimer != nil {
		s.flushTimer.Stop()
		s.flushTimer = nil
	}
}

func (s *SkyDDSSender) drainBatchLocked() [][]byte {
	if len(s.batch) == 0 {
		return nil
	}
	out := s.batch
	s.batch = nil
	s.batchBytes = 0
	return out
}

func (s *SkyDDSSender) restoreBatchLocked(payloads [][]byte) {
	if len(payloads) == 0 {
		return
	}
	restored := make([][]byte, 0, len(payloads)+len(s.batch))
	restored = append(restored, payloads...)
	restored = append(restored, s.batch...)
	s.batch = restored
	s.batchBytes = 0
	for i := range s.batch {
		s.batchBytes += len(s.batch[i])
	}
	if len(s.batch) > 0 {
		s.startFlushTimerLocked()
	}
}

func (s *SkyDDSSender) flushByTimer() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	flushNow := s.drainBatchLocked()
	s.flushTimer = nil
	s.mu.Unlock()

	if len(flushNow) == 0 {
		return nil
	}
	if err := s.flushPayloads(flushNow, "delay"); err != nil {
		s.mu.Lock()
		s.restoreBatchLocked(flushNow)
		s.mu.Unlock()
		logx.L().Warnw("SkyDDS batch timer flush failed; batch restored for retry", "sender", s.name, "batch_count", len(flushNow), "error", err)
		return err
	}
	return nil
}

func (s *SkyDDSSender) flushPayloads(payloads [][]byte, reason string) error {
	if len(payloads) == 0 {
		return nil
	}
	if err := s.writer.WriteBatch(payloads); err != nil {
		logx.L().Warnw("SkyDDS batch send failed", "sender", s.name, "reason", reason, "batch_count", len(payloads), "error", err)
		return err
	}
	return nil
}

func (s *SkyDDSSender) Close(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	s.closed = true
	flushNow := s.drainBatchLocked()
	s.stopFlushTimerLocked()
	s.mu.Unlock()

	if len(flushNow) > 0 {
		if err := s.flushPayloads(flushNow, "close"); err != nil {
			return err
		}
	}
	return s.writer.Close()
}
