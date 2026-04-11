package receiver

import (
	"context"
	"testing"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/packet"
)

func TestLocalTimerReceiverIntervalTotalPacketsAndMeta(t *testing.T) {
	r, err := NewLocalTimerReceiver("rx_local", config.ReceiverConfig{
		Type: "local_timer",
		MatchKey: config.ReceiverMatchKeyConfig{
			Mode:       "fixed",
			FixedValue: "chain-monitor",
		},
		Generator: config.LocalGeneratorConfig{
			Interval:      "5ms",
			PayloadFormat: "hex",
			PayloadData:   "0102ff",
			TotalPackets:  3,
			Remote:        "local-generator",
			Local:         "node-a",
		},
	})
	if err != nil {
		t.Fatalf("new local timer receiver: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var got []*packet.Packet
	err = r.Start(ctx, func(pkt *packet.Packet) {
		got = append(got, pkt)
	})
	if err != nil {
		t.Fatalf("start local timer receiver: %v", err)
	}
	defer func() {
		for _, pkt := range got {
			pkt.Release()
		}
	}()

	if len(got) != 3 {
		t.Fatalf("packet count mismatch: got=%d want=3", len(got))
	}
	for _, pkt := range got {
		if string(pkt.Payload) != string([]byte{0x01, 0x02, 0xff}) {
			t.Fatalf("payload mismatch: got=%x", pkt.Payload)
		}
		if pkt.Kind != packet.PayloadKindStream {
			t.Fatalf("kind mismatch: got=%d", pkt.Kind)
		}
		if pkt.Meta.Proto != packet.ProtoLocal {
			t.Fatalf("proto mismatch: got=%d", pkt.Meta.Proto)
		}
		if pkt.Meta.ReceiverName != "rx_local" {
			t.Fatalf("receiver name mismatch: got=%q", pkt.Meta.ReceiverName)
		}
		if pkt.Meta.MatchKey != "local|fixed=chain-monitor" {
			t.Fatalf("match key mismatch: got=%q", pkt.Meta.MatchKey)
		}
		if pkt.Meta.Remote != "local-generator" || pkt.Meta.Local != "node-a" {
			t.Fatalf("addr meta mismatch: remote=%q local=%q", pkt.Meta.Remote, pkt.Meta.Local)
		}
	}
}

func TestLocalTimerReceiverCancelStopsStart(t *testing.T) {
	r, err := NewLocalTimerReceiver("rx_local", config.ReceiverConfig{
		Type: "local_timer",
		MatchKey: config.ReceiverMatchKeyConfig{
			Mode:       "fixed",
			FixedValue: "load-test",
		},
		Generator: config.LocalGeneratorConfig{
			RatePerSec:    1000,
			TickInterval:  "5ms",
			PayloadFormat: "text",
			PayloadData:   "ping",
		},
	})
	if err != nil {
		t.Fatalf("new local timer receiver: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- r.Start(ctx, func(pkt *packet.Packet) {
			pkt.Release()
		})
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("start returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("local_timer start did not exit after cancel")
	}
}
