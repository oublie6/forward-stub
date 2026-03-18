package sender

import (
	"sync/atomic"
	"testing"
)

// TestNextShardIndexPowerOfTwoRoundRobin 验证 sender 包中 NextShardIndexPowerOfTwoRoundRobin 的行为。
func TestNextShardIndexPowerOfTwoRoundRobin(t *testing.T) {
	var next atomic.Uint64
	got := []int{
		nextShardIndex(&next, 3),
		nextShardIndex(&next, 3),
		nextShardIndex(&next, 3),
		nextShardIndex(&next, 3),
		nextShardIndex(&next, 3),
	}
	want := []int{1, 2, 3, 0, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx[%d]=%d want=%d", i, got[i], want[i])
		}
	}
}

// TestNextShardIndexSingleShardAlwaysZero 验证 sender 包中 NextShardIndexSingleShardAlwaysZero 的行为。
func TestNextShardIndexSingleShardAlwaysZero(t *testing.T) {
	var next atomic.Uint64
	got := []int{
		nextShardIndex(&next, 0),
		nextShardIndex(&next, 0),
		nextShardIndex(&next, 0),
	}
	want := []int{0, 0, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx[%d]=%d want=%d", i, got[i], want[i])
		}
	}
}

// TestNextShardIndexUsesBitMaskWhenCounterOutOfBound 验证 sender 包中 NextShardIndexUsesBitMaskWhenCounterOutOfBound 的行为。
func TestNextShardIndexUsesBitMaskWhenCounterOutOfBound(t *testing.T) {
	var next atomic.Uint64
	next.Store(100)
	if got := nextShardIndex(&next, 3); got != 1 {
		t.Fatalf("idx=%d want=1", got)
	}
	if got := nextShardIndex(&next, 3); got != 2 {
		t.Fatalf("idx=%d want=2", got)
	}
	if got := nextShardIndex(&next, 3); got != 3 {
		t.Fatalf("idx=%d want=3", got)
	}
}

// TestKafkaNextShardIndexRoundRobin 验证 sender 包中 KafkaNextShardIndexRoundRobin 的行为。
func TestKafkaNextShardIndexRoundRobin(t *testing.T) {
	s := &KafkaSender{concurrency: 4, shardMask: 3}
	got := []int{
		nextShardIndex(&s.nextIdx, s.shardMask),
		nextShardIndex(&s.nextIdx, s.shardMask),
		nextShardIndex(&s.nextIdx, s.shardMask),
		nextShardIndex(&s.nextIdx, s.shardMask),
		nextShardIndex(&s.nextIdx, s.shardMask),
	}
	want := []int{1, 2, 3, 0, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx[%d]=%d want=%d", i, got[i], want[i])
		}
	}
}

// TestUDPPickShardRoundRobin 验证 sender 包中 UDPPickShardRoundRobin 的行为。
func TestUDPPickShardRoundRobin(t *testing.T) {
	u := &UDPUnicastSender{concurrency: 2, shardMask: 1}
	m := &UDPMulticastSender{concurrency: 2, shardMask: 1}
	if nextShardIndex(&u.nextIdx, u.shardMask) != 1 || nextShardIndex(&u.nextIdx, u.shardMask) != 0 || nextShardIndex(&u.nextIdx, u.shardMask) != 1 {
		t.Fatalf("udp unicast round robin failed")
	}
	if nextShardIndex(&m.nextIdx, m.shardMask) != 1 || nextShardIndex(&m.nextIdx, m.shardMask) != 0 || nextShardIndex(&m.nextIdx, m.shardMask) != 1 {
		t.Fatalf("udp multicast round robin failed")
	}
}

// TestSFTPPickShardTransferAffinityWithRoundRobinAssignment 验证 sender 包中 SFTPPickShardTransferAffinityWithRoundRobinAssignment 的行为。
func TestSFTPPickShardTransferAffinityWithRoundRobinAssignment(t *testing.T) {
	s := &SFTPSender{concurrency: 4, shardMask: 3, transferShard: map[string]int{}}
	if got := s.pickShard("t1"); got != 1 {
		t.Fatalf("t1 first shard=%d want=1", got)
	}
	if got := s.pickShard("t2"); got != 2 {
		t.Fatalf("t2 first shard=%d want=2", got)
	}
	if got := s.pickShard("t1"); got != 1 {
		t.Fatalf("t1 should keep affinity, got=%d", got)
	}
	if got := s.pickShard("t3"); got != 3 {
		t.Fatalf("t3 first shard=%d want=3", got)
	}
}
