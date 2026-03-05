package main

import (
	"context"
	"testing"
	"time"
)

func TestRunForwardBenchmarkChannelZeroLoss(t *testing.T) {
	ctx := context.Background()
	res, err := runForwardBenchmark(
		ctx,
		"udp",
		1500*time.Millisecond,
		500*time.Millisecond,
		256,
		2,
		2000,
		true,
		2,
		4<<20,
		false,
		1024,
		"channel",
	)
	if err != nil {
		t.Fatalf("run benchmark: %v", err)
	}
	if res.packetLossRate != 0 {
		t.Fatalf("expected zero loss, got loss_rate=%f (sent=%d recv=%d)", res.packetLossRate, res.sentPackets, res.recvPackets)
	}
	if res.pps <= 0 {
		t.Fatalf("expected positive throughput, got pps=%f", res.pps)
	}
}
